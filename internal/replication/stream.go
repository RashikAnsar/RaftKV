package replication

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/cdc"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// StreamServer implements the gRPC ReplicationService server
type StreamServer struct {
	pb.UnimplementedReplicationServiceServer

	publisher    *cdc.Publisher
	datacenterID string
	logger       *zap.Logger

	mu      sync.RWMutex
	streams map[string]*activeStream // Map of datacenter ID to active stream
}

// activeStream tracks an active streaming connection
type activeStream struct {
	datacenterID string
	fromIndex    uint64
	startTime    time.Time
	eventsSent   uint64
}

// NewStreamServer creates a new gRPC streaming server
func NewStreamServer(publisher *cdc.Publisher, datacenterID string, logger *zap.Logger) *StreamServer {
	return &StreamServer{
		publisher:    publisher,
		datacenterID: datacenterID,
		logger:       logger,
		streams:      make(map[string]*activeStream),
	}
}

// StreamChanges implements the bidirectional streaming RPC
func (s *StreamServer) StreamChanges(req *pb.StreamRequest, stream pb.ReplicationService_StreamChangesServer) error {
	ctx := stream.Context()

	s.logger.Info("New replication stream started",
		zap.String("from_dc", req.DatacenterId),
		zap.Uint64("from_index", req.FromIndex),
		zap.String("key_prefix", req.KeyPrefix),
	)

	// Register this stream
	s.mu.Lock()
	activeStr := &activeStream{
		datacenterID: req.DatacenterId,
		fromIndex:    req.FromIndex,
		startTime:    time.Now(),
	}
	s.streams[req.DatacenterId] = activeStr
	s.mu.Unlock()

	// Cleanup on exit
	defer func() {
		s.mu.Lock()
		delete(s.streams, req.DatacenterId)
		s.mu.Unlock()

		s.logger.Info("Replication stream closed",
			zap.String("from_dc", req.DatacenterId),
			zap.Uint64("events_sent", activeStr.eventsSent),
			zap.Duration("duration", time.Since(activeStr.startTime)),
		)
	}()

	// Subscribe to CDC events
	var filter cdc.EventFilter
	if req.KeyPrefix != "" {
		filter = cdc.KeyPrefixFilter(req.KeyPrefix)
	}

	subscriber := s.publisher.Subscribe(
		fmt.Sprintf("stream-%s", req.DatacenterId),
		1000, // Buffer 1000 events
		filter,
	)
	defer s.publisher.Unsubscribe(subscriber.ID())

	// Stream events to client
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		default:
			// Receive event from subscriber
			event, err := subscriber.Recv(ctx)
			if err != nil {
				if err == cdc.ErrSubscriberClosed || err == io.EOF {
					return nil
				}
				if err == context.Canceled || err == context.DeadlineExceeded {
					return err
				}
				s.logger.Error("Error receiving CDC event", zap.Error(err))
				continue
			}

			// Skip events before requested index
			if event.RaftIndex <= req.FromIndex {
				continue
			}

			// Convert to protobuf message
			pbEvent := &pb.ChangeEvent{
				RaftIndex:    event.RaftIndex,
				RaftTerm:     event.RaftTerm,
				Operation:    event.Operation,
				Key:          event.Key,
				Value:        event.Value,
				Timestamp:    timestamppb.New(event.Timestamp),
				DatacenterId: event.DatacenterID,
				SequenceNum:  event.SequenceNum,
				VectorClock:  event.VectorClock,
			}

			// Send to client
			if err := stream.Send(pbEvent); err != nil {
				s.logger.Error("Failed to send event to client",
					zap.String("client_dc", req.DatacenterId),
					zap.Error(err),
				)
				return err
			}

			activeStr.eventsSent++

			if activeStr.eventsSent%1000 == 0 {
				s.logger.Debug("Stream progress",
					zap.String("client_dc", req.DatacenterId),
					zap.Uint64("events_sent", activeStr.eventsSent),
					zap.Uint64("last_index", event.RaftIndex),
				)
			}
		}
	}
}

// GetReplicationStatus returns the current replication status
func (s *StreamServer) GetReplicationStatus(ctx context.Context, req *pb.StatusRequest) (*pb.ReplicationStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := s.publisher.Stats()

	status := &pb.ReplicationStatus{
		DatacenterId:     s.datacenterID,
		LastIndex:        stats.SequenceNum,
		Health:           pb.HealthStatus_HEALTH_HEALTHY,
		EventsReplicated: stats.SequenceNum,
		LagByDc:          make(map[string]*pb.DatacenterLag),
	}

	// Add lag info for each active stream
	for dcID, stream := range s.streams {
		status.LagByDc[dcID] = &pb.DatacenterLag{
			DatacenterId:        dcID,
			LastReplicatedIndex: stream.fromIndex,
			Connected:           true,
			LastSuccess:         timestamppb.New(stream.startTime),
		}
	}

	s.logger.Debug("Replication status requested",
		zap.String("requester", req.DatacenterId),
		zap.Int("active_streams", len(s.streams)),
	)

	return status, nil
}

// Heartbeat implements connection health checking
func (s *StreamServer) Heartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	return &pb.HeartbeatResponse{
		DatacenterId: s.datacenterID,
		Timestamp:    timestamppb.New(time.Now()),
		Health:       pb.HealthStatus_HEALTH_HEALTHY,
	}, nil
}

// ActiveStreams returns the number of active streaming connections
func (s *StreamServer) ActiveStreams() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.streams)
}

// GetStreamInfo returns information about a specific stream
func (s *StreamServer) GetStreamInfo(datacenterID string) (*activeStream, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stream, exists := s.streams[datacenterID]
	return stream, exists
}
