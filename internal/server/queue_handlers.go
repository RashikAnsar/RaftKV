package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/queue"
)

// QueueHandlers handles queue-related HTTP operations
type QueueHandlers struct {
	queueManager *queue.QueueManager
	logger       *zap.Logger
}

// NewQueueHandlers creates a new queue handlers instance
func NewQueueHandlers(queueManager *queue.QueueManager, logger *zap.Logger) *QueueHandlers {
	return &QueueHandlers{
		queueManager: queueManager,
		logger:       logger,
	}
}

// RegisterRoutes registers queue-related routes
func (h *QueueHandlers) RegisterRoutes(router *mux.Router, maxRequestSize int64) {
	// Queue management endpoints
	router.HandleFunc("/queues", h.handleListQueues).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/queues", limitRequestSize(h.handleCreateQueue, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}", h.handleGetQueueInfo).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/queues/{name}", h.handleDeleteQueue).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/queues/{name}/stats", h.handleGetQueueStats).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/queues/{name}/purge", limitRequestSize(h.handlePurgeQueue, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)

	// Message operations endpoints
	router.HandleFunc("/queues/{name}/messages", limitRequestSize(h.handleEnqueue, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/dequeue", h.handleDequeue).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/peek", h.handlePeek).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/{id}", h.handleGetMessage).Methods(http.MethodGet, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/{id}", h.handleDeleteMessage).Methods(http.MethodDelete, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/{id}/ack", h.handleAcknowledge).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/{id}/nack", limitRequestSize(h.handleNack, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)

	// Batch operations endpoints
	router.HandleFunc("/queues/{name}/messages/batch", limitRequestSize(h.handleBatchEnqueue, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/batch/dequeue", limitRequestSize(h.handleBatchDequeue, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
	router.HandleFunc("/queues/{name}/messages/batch/ack", limitRequestSize(h.handleBatchAcknowledge, maxRequestSize)).Methods(http.MethodPost, http.MethodOptions)
}

// Queue management handlers

func (h *QueueHandlers) handleListQueues(w http.ResponseWriter, r *http.Request) {
	queues := h.queueManager.ListQueues()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"queues": queues,
		"count":  len(queues),
	})
}

func (h *QueueHandlers) handleCreateQueue(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name                string `json:"name"`
		Type                string `json:"type"`
		MaxLength           int    `json:"max_length"`
		MaxRetries          int    `json:"max_retries"`
		MessageTTLMs        int64  `json:"message_ttl_ms"`
		VisibilityTimeoutMs int64  `json:"visibility_timeout_ms"`
		DLQEnabled          bool   `json:"dlq_enabled"`
		DLQName             string `json:"dlq_name"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "queue name is required")
		return
	}

	// Set defaults
	if req.Type == "" {
		req.Type = "fifo"
	}
	if req.VisibilityTimeoutMs == 0 {
		req.VisibilityTimeoutMs = 30000 // 30 seconds
	}

	config := &queue.QueueConfig{
		Name:              req.Name,
		Type:              req.Type,
		MaxLength:         req.MaxLength,
		MaxRetries:        req.MaxRetries,
		MessageTTL:        time.Duration(req.MessageTTLMs) * time.Millisecond,
		VisibilityTimeout: time.Duration(req.VisibilityTimeoutMs) * time.Millisecond,
		DLQEnabled:        req.DLQEnabled,
		DLQName:           req.DLQName,
	}

	if err := h.queueManager.CreateQueue(config); err != nil {
		h.logger.Error("Failed to create queue",
			zap.String("queue", req.Name),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "queue created",
		"config":  config,
	})
}

func (h *QueueHandlers) handleGetQueueInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	config, err := h.queueManager.GetQueueConfig(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "queue not found")
		return
	}

	q, err := h.queueManager.GetQueue(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "queue not found")
		return
	}

	stats := q.Stats()

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"config": config,
		"stats":  stats,
	})
}

func (h *QueueHandlers) handleDeleteQueue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	if err := h.queueManager.DeleteQueue(name); err != nil {
		h.logger.Error("Failed to delete queue",
			zap.String("queue", name),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "queue deleted",
		"name":    name,
	})
}

func (h *QueueHandlers) handleGetQueueStats(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	q, err := h.queueManager.GetQueue(name)
	if err != nil {
		respondError(w, http.StatusNotFound, "queue not found")
		return
	}

	stats := q.Stats()

	respondJSON(w, http.StatusOK, stats)
}

func (h *QueueHandlers) handlePurgeQueue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	name := vars["name"]

	if err := h.queueManager.Purge(name); err != nil {
		h.logger.Error("Failed to purge queue",
			zap.String("queue", name),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "queue purged",
		"name":    name,
	})
}

// Message operation handlers

func (h *QueueHandlers) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	var req struct {
		Body        []byte            `json:"body"`
		Priority    int               `json:"priority"`
		Headers     map[string]string `json:"headers"`
		ScheduledAt *int64            `json:"scheduled_at"` // Unix timestamp in milliseconds
		ExpiresAt   *int64            `json:"expires_at"`   // Unix timestamp in milliseconds
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Body == nil {
		respondError(w, http.StatusBadRequest, "message body is required")
		return
	}

	msg := queue.NewMessage(queueName, req.Body)
	msg.Priority = req.Priority
	if req.Headers != nil {
		msg.Headers = req.Headers
	}
	if req.ScheduledAt != nil {
		scheduledAt := time.UnixMilli(*req.ScheduledAt)
		msg.ScheduledAt = &scheduledAt
	}
	if req.ExpiresAt != nil {
		expiresAt := time.UnixMilli(*req.ExpiresAt)
		msg.ExpiresAt = &expiresAt
	}

	if err := h.queueManager.Enqueue(queueName, msg); err != nil {
		h.logger.Error("Failed to enqueue message",
			zap.String("queue", queueName),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]interface{}{
		"message_id": msg.ID,
		"queue":      queueName,
	})
}

func (h *QueueHandlers) handleDequeue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	msg, err := h.queueManager.Dequeue(queueName)
	if err != nil {
		if err.Error() == "queue is empty" || err.Error() == "queue not found: "+queueName {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		h.logger.Error("Failed to dequeue message",
			zap.String("queue", queueName),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, msg)
}

func (h *QueueHandlers) handlePeek(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	msg, err := h.queueManager.Peek(queueName)
	if err != nil {
		if err.Error() == "queue is empty" || err.Error() == "queue not found: "+queueName {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		h.logger.Error("Failed to peek message",
			zap.String("queue", queueName),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, msg)
}

func (h *QueueHandlers) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]
	messageID := vars["id"]

	msg, err := h.queueManager.Get(queueName, messageID)
	if err != nil {
		respondError(w, http.StatusNotFound, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, msg)
}

func (h *QueueHandlers) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]
	messageID := vars["id"]

	if err := h.queueManager.Delete(queueName, messageID); err != nil {
		h.logger.Error("Failed to delete message",
			zap.String("queue", queueName),
			zap.String("message_id", messageID),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "message deleted",
		"id":      messageID,
	})
}

func (h *QueueHandlers) handleAcknowledge(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]
	messageID := vars["id"]

	if err := h.queueManager.Acknowledge(queueName, messageID); err != nil {
		h.logger.Error("Failed to acknowledge message",
			zap.String("queue", queueName),
			zap.String("message_id", messageID),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "message acknowledged",
		"id":      messageID,
	})
}

func (h *QueueHandlers) handleNack(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]
	messageID := vars["id"]

	var req struct {
		Requeue bool `json:"requeue"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	// Empty body is allowed - defaults to requeue=false
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	if err := h.queueManager.Nack(queueName, messageID, req.Requeue); err != nil {
		h.logger.Error("Failed to nack message",
			zap.String("queue", queueName),
			zap.String("message_id", messageID),
			zap.Bool("requeue", req.Requeue),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "message nacked",
		"id":      messageID,
		"requeue": req.Requeue,
	})
}

// Batch operation handlers

func (h *QueueHandlers) handleBatchEnqueue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	var req struct {
		Messages []struct {
			Body        []byte            `json:"body"`
			Priority    int               `json:"priority"`
			Headers     map[string]string `json:"headers"`
			ScheduledAt *int64            `json:"scheduled_at"`
			ExpiresAt   *int64            `json:"expires_at"`
		} `json:"messages"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	messages := make([]*queue.Message, 0, len(req.Messages))
	for _, msgReq := range req.Messages {
		msg := queue.NewMessage(queueName, msgReq.Body)
		msg.Priority = msgReq.Priority
		if msgReq.Headers != nil {
			msg.Headers = msgReq.Headers
		}
		if msgReq.ScheduledAt != nil {
			scheduledAt := time.UnixMilli(*msgReq.ScheduledAt)
			msg.ScheduledAt = &scheduledAt
		}
		if msgReq.ExpiresAt != nil {
			expiresAt := time.UnixMilli(*msgReq.ExpiresAt)
			msg.ExpiresAt = &expiresAt
		}
		messages = append(messages, msg)
	}

	errors, err := h.queueManager.BatchEnqueue(queueName, messages)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	successful := 0
	failed := 0
	results := make([]map[string]interface{}, len(messages))
	for i, msg := range messages {
		if errors[i] == nil {
			successful++
			results[i] = map[string]interface{}{
				"success":    true,
				"message_id": msg.ID,
			}
		} else {
			failed++
			results[i] = map[string]interface{}{
				"success": false,
				"error":   errors[i].Error(),
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results":    results,
		"successful": successful,
		"failed":     failed,
	})
}

func (h *QueueHandlers) handleBatchDequeue(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	var req struct {
		Count int `json:"count"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Count <= 0 {
		req.Count = 1
	}
	if req.Count > 100 {
		req.Count = 100 // Limit batch size
	}

	messages, err := h.queueManager.BatchDequeue(queueName, req.Count)
	if err != nil {
		h.logger.Error("Failed to batch dequeue",
			zap.String("queue", queueName),
			zap.Int("count", req.Count),
			zap.Error(err),
		)
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
	})
}

func (h *QueueHandlers) handleBatchAcknowledge(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	queueName := vars["name"]

	var req struct {
		MessageIDs []string `json:"message_ids"`
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if err := json.Unmarshal(body, &req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	errors, err := h.queueManager.BatchAcknowledge(queueName, req.MessageIDs)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	successful := 0
	failed := 0
	results := make([]map[string]interface{}, len(req.MessageIDs))
	for i, msgID := range req.MessageIDs {
		if errors[i] == nil {
			successful++
			results[i] = map[string]interface{}{
				"success":    true,
				"message_id": msgID,
			}
		} else {
			failed++
			results[i] = map[string]interface{}{
				"success":    false,
				"message_id": msgID,
				"error":      errors[i].Error(),
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"results":    results,
		"successful": successful,
		"failed":     failed,
	})
}

// Helper to parse integer query parameter
func parseIntQueryParam(r *http.Request, param string, defaultValue int) int {
	valueStr := r.URL.Query().Get(param)
	if valueStr == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(valueStr)
	if err != nil {
		return defaultValue
	}
	return value
}
