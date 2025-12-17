package server

import (
	"context"
	"fmt"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/watch"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Watch implements the gRPC Watch RPC for streaming key-value change notifications
func (s *GRPCServer) Watch(req *pb.WatchRequest, stream pb.KVStore_WatchServer) error {
	// Validate request
	if req == nil {
		return status.Error(codes.InvalidArgument, "watch request cannot be nil")
	}

	// Log watch request
	s.logger.Info("Watch request received",
		zap.String("key_prefix", req.KeyPrefix),
		zap.Strings("operations", req.Operations),
		zap.Bool("send_initial_value", req.SendInitialValue),
	)

	// Check if watch manager is initialized
	if s.watchManager == nil {
		s.logger.Error("Watch manager not initialized")
		return status.Error(codes.Internal, "watch service not available")
	}

	// Create internal watch request
	watchReq := &watch.WatchRequest{
		KeyPrefix:        req.KeyPrefix,
		Operations:       req.Operations,
		StartRevision:    req.StartRevision,
		SendInitialValue: req.SendInitialValue,
	}

	// Create watch subscription
	watcher, err := s.watchManager.CreateWatch(stream.Context(), watchReq)
	if err != nil {
		s.logger.Error("Failed to create watch",
			zap.String("key_prefix", req.KeyPrefix),
			zap.Error(err),
		)

		if err == watch.ErrTooManyWatchers {
			return status.Error(codes.ResourceExhausted, "too many active watchers")
		}

		return status.Error(codes.Internal, fmt.Sprintf("failed to create watch: %v", err))
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

	s.logger.Info("Watch created",
		zap.String("watch_id", watcher.GetID()),
		zap.String("key_prefix", req.KeyPrefix),
	)

	// Send initial values if requested
	if req.SendInitialValue {
		initialEvents, err := watcher.FetchInitialValues(stream.Context())
		if err != nil {
			s.logger.Error("Failed to fetch initial values",
				zap.String("watch_id", watcher.GetID()),
				zap.Error(err),
			)
			return status.Error(codes.Internal, "failed to fetch initial values")
		}

		// Send initial events
		for _, event := range initialEvents {
			pbEvent := &pb.WatchEvent{
				Key:       event.Key,
				Value:     event.Value,
				Operation: event.Operation,
				RaftIndex: event.RaftIndex,
				RaftTerm:  event.RaftTerm,
				Timestamp: event.Timestamp,
				Version:   event.Version,
			}

			if err := stream.Send(pbEvent); err != nil {
				s.logger.Error("Failed to send initial watch event",
					zap.String("watch_id", watcher.GetID()),
					zap.String("key", event.Key),
					zap.Error(err),
				)
				return status.Error(codes.Internal, "failed to send initial event")
			}
		}

		s.logger.Debug("Sent initial values",
			zap.String("watch_id", watcher.GetID()),
			zap.Int("count", len(initialEvents)),
		)
	}

	// Stream events
	eventsSent := uint64(0)
	for {
		// Check if context is canceled
		select {
		case <-stream.Context().Done():
			s.logger.Info("Watch stream context canceled",
				zap.String("watch_id", watcher.GetID()),
				zap.Uint64("events_sent", eventsSent),
			)
			return stream.Context().Err()
		default:
		}

		// Receive next event from watcher
		event, err := watcher.Recv(stream.Context())
		if err != nil {
			if err == context.Canceled || err == context.DeadlineExceeded {
				s.logger.Info("Watch stream closed",
					zap.String("watch_id", watcher.GetID()),
					zap.Uint64("events_sent", eventsSent),
				)
				return nil
			}

			s.logger.Error("Failed to receive watch event",
				zap.String("watch_id", watcher.GetID()),
				zap.Error(err),
			)
			return status.Error(codes.Internal, "failed to receive event")
		}

		// Convert to protobuf event
		pbEvent := &pb.WatchEvent{
			Key:       event.Key,
			Value:     event.Value,
			Operation: event.Operation,
			RaftIndex: event.RaftIndex,
			RaftTerm:  event.RaftTerm,
			Timestamp: event.Timestamp,
			Version:   event.Version,
		}

		// Send event to client
		if err := stream.Send(pbEvent); err != nil {
			s.logger.Error("Failed to send watch event",
				zap.String("watch_id", watcher.GetID()),
				zap.String("key", event.Key),
				zap.Error(err),
			)
			return status.Error(codes.Internal, "failed to send event")
		}

		eventsSent++

		// Log progress every 100 events
		if eventsSent%100 == 0 {
			s.logger.Debug("Watch progress",
				zap.String("watch_id", watcher.GetID()),
				zap.Uint64("events_sent", eventsSent),
			)
		}
	}
}
