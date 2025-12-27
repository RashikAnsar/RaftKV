package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/RashikAnsar/raftkv/internal/watch"
	"go.uber.org/zap"
)

// handleWatch handles HTTP Server-Sent Events (SSE) watch requests
func (s *HTTPServer) handleWatch(w http.ResponseWriter, r *http.Request) {
	// Check if watch manager is initialized
	if s.watchManager == nil {
		s.logger.Error("Watch manager not initialized")
		s.respondError(w, http.StatusServiceUnavailable, "watch service not available")
		return
	}

	// Parse query parameters
	keyPrefix := r.URL.Query().Get("prefix")
	operationsStr := r.URL.Query().Get("operations")
	sendInitialValueStr := r.URL.Query().Get("send_initial_value")

	// Parse operations filter
	var operations []string
	if operationsStr != "" {
		operations = strings.Split(operationsStr, ",")
		// Trim whitespace from each operation
		for i := range operations {
			operations[i] = strings.TrimSpace(operations[i])
		}
	}

	// Parse send_initial_value flag
	sendInitialValue := sendInitialValueStr == "true" || sendInitialValueStr == "1"

	s.logger.Info("SSE watch request received",
		zap.String("key_prefix", keyPrefix),
		zap.Strings("operations", operations),
		zap.Bool("send_initial_value", sendInitialValue),
		zap.String("remote_addr", r.RemoteAddr),
	)

	// Get flusher for streaming - check BEFORE setting headers
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error("ResponseWriter does not support flushing")
		s.respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*") // Allow CORS for browser clients
	w.Header().Set("X-Accel-Buffering", "no")          // Disable nginx buffering

	// Send initial comment to establish connection
	fmt.Fprintf(w, ": SSE watch stream established\n\n")
	flusher.Flush()

	// Create watch request
	watchReq := &watch.WatchRequest{
		KeyPrefix:        keyPrefix,
		Operations:       operations,
		StartRevision:    0,
		SendInitialValue: sendInitialValue,
	}

	// Create watch subscription
	watcher, err := s.watchManager.CreateWatch(r.Context(), watchReq)
	if err != nil {
		s.logger.Error("Failed to create watch",
			zap.String("key_prefix", keyPrefix),
			zap.Error(err),
		)

		// Send error event
		s.sendSSEError(w, flusher, "failed to create watch", err)
		return
	}

	// Ensure cleanup on exit
	defer func() {
		if err := s.watchManager.CancelWatch(watcher.GetID()); err != nil {
			s.logger.Warn("Failed to cancel watch on stream close",
				zap.String("watch_id", watcher.GetID()),
				zap.Error(err),
			)
		}
	}()

	s.logger.Info("SSE watch created",
		zap.String("watch_id", watcher.GetID()),
		zap.String("key_prefix", keyPrefix),
	)

	// Send initial values if requested
	if sendInitialValue {
		initialEvents, err := watcher.FetchInitialValues(r.Context())
		if err != nil {
			s.logger.Error("Failed to fetch initial values",
				zap.String("watch_id", watcher.GetID()),
				zap.Error(err),
			)
			s.sendSSEError(w, flusher, "failed to fetch initial values", err)
			return
		}

		// Send initial events
		for _, event := range initialEvents {
			if err := s.sendSSEEvent(w, flusher, event); err != nil {
				s.logger.Error("Failed to send initial SSE event",
					zap.String("watch_id", watcher.GetID()),
					zap.String("key", event.Key),
					zap.Error(err),
				)
				return
			}
		}

		s.logger.Debug("Sent initial values via SSE",
			zap.String("watch_id", watcher.GetID()),
			zap.Int("count", len(initialEvents)),
		)
	}

	// Stream events
	eventsSent := uint64(0)
	ticker := time.NewTicker(30 * time.Second) // Heartbeat to keep connection alive
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			s.logger.Info("SSE watch stream context canceled",
				zap.String("watch_id", watcher.GetID()),
				zap.Uint64("events_sent", eventsSent),
			)
			return

		case <-ticker.C:
			// Send heartbeat comment to keep connection alive
			fmt.Fprintf(w, ": heartbeat\n\n")
			flusher.Flush()

		default:
			// Try to receive event with timeout
			event, err := watcher.RecvWithTimeout(1 * time.Second)
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					s.logger.Info("SSE watch stream closed",
						zap.String("watch_id", watcher.GetID()),
						zap.Uint64("events_sent", eventsSent),
					)
					return
				}

				// Timeout is expected, continue to next iteration
				if strings.Contains(err.Error(), "timeout") {
					continue
				}

				s.logger.Error("Failed to receive watch event",
					zap.String("watch_id", watcher.GetID()),
					zap.Error(err),
				)
				s.sendSSEError(w, flusher, "failed to receive event", err)
				return
			}

			// Send event to client
			if err := s.sendSSEEvent(w, flusher, event); err != nil {
				s.logger.Error("Failed to send SSE event",
					zap.String("watch_id", watcher.GetID()),
					zap.String("key", event.Key),
					zap.Error(err),
				)
				return
			}

			eventsSent++

			// Log progress every 100 events
			if eventsSent%100 == 0 {
				s.logger.Debug("SSE watch progress",
					zap.String("watch_id", watcher.GetID()),
					zap.Uint64("events_sent", eventsSent),
				)
			}
		}
	}
}

// sendSSEEvent sends a watch event in SSE format
func (s *HTTPServer) sendSSEEvent(w http.ResponseWriter, flusher http.Flusher, event *watch.WatchEvent) error {
	// Convert event to JSON
	eventData := map[string]interface{}{
		"key":        event.Key,
		"value":      string(event.Value), // Convert to string for JSON
		"operation":  event.Operation,
		"raft_index": event.RaftIndex,
		"raft_term":  event.RaftTerm,
		"timestamp":  event.Timestamp,
		"version":    event.Version,
	}

	jsonData, err := json.Marshal(eventData)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Send SSE event
	// Format: "event: watch\ndata: {json}\n\n"
	fmt.Fprintf(w, "event: watch\n")
	fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
	flusher.Flush()

	return nil
}

// sendSSEError sends an error event in SSE format
func (s *HTTPServer) sendSSEError(w http.ResponseWriter, flusher http.Flusher, message string, err error) {
	errorData := map[string]interface{}{
		"message": message,
		"error":   err.Error(),
	}

	jsonData, _ := json.Marshal(errorData)

	fmt.Fprintf(w, "event: error\n")
	fmt.Fprintf(w, "data: %s\n\n", string(jsonData))
	flusher.Flush()
}
