package server

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/queue"
)

// QueueGRPCServer implements the QueueService gRPC service
type QueueGRPCServer struct {
	pb.UnimplementedQueueServiceServer
	queueManager *queue.QueueManager
	logger       *zap.Logger
}

// NewQueueGRPCServer creates a new gRPC queue server
func NewQueueGRPCServer(queueManager *queue.QueueManager, logger *zap.Logger) *QueueGRPCServer {
	return &QueueGRPCServer{
		queueManager: queueManager,
		logger:       logger,
	}
}

// Queue management operations

func (s *QueueGRPCServer) CreateQueue(ctx context.Context, req *pb.CreateQueueRequest) (*pb.CreateQueueResponse, error) {
	if req.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "queue config is required")
	}

	if req.Config.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	config := &queue.QueueConfig{
		Name:              req.Config.Name,
		Type:              req.Config.Type,
		MaxLength:         int(req.Config.MaxLength),
		MaxRetries:        int(req.Config.MaxRetries),
		MessageTTL:        time.Duration(req.Config.MessageTtlMs) * time.Millisecond,
		VisibilityTimeout: time.Duration(req.Config.VisibilityTimeoutMs) * time.Millisecond,
		DLQEnabled:        req.Config.DlqEnabled,
		DLQName:           req.Config.DlqName,
	}

	if err := s.queueManager.CreateQueue(config); err != nil {
		s.logger.Error("Failed to create queue",
			zap.String("queue", config.Name),
			zap.Error(err),
		)
		return &pb.CreateQueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.CreateQueueResponse{
		Success: true,
		Config:  req.Config,
	}, nil
}

func (s *QueueGRPCServer) DeleteQueue(ctx context.Context, req *pb.DeleteQueueRequest) (*pb.DeleteQueueResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if err := s.queueManager.DeleteQueue(req.Name); err != nil {
		s.logger.Error("Failed to delete queue",
			zap.String("queue", req.Name),
			zap.Error(err),
		)
		return &pb.DeleteQueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.DeleteQueueResponse{
		Success: true,
	}, nil
}

func (s *QueueGRPCServer) GetQueueInfo(ctx context.Context, req *pb.GetQueueInfoRequest) (*pb.GetQueueInfoResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	config, err := s.queueManager.GetQueueConfig(req.Name)
	if err != nil {
		return &pb.GetQueueInfoResponse{
			Found: false,
		}, nil
	}

	q, err := s.queueManager.GetQueue(req.Name)
	if err != nil {
		return &pb.GetQueueInfoResponse{
			Found: false,
		}, nil
	}

	stats := q.Stats()

	return &pb.GetQueueInfoResponse{
		Config: &pb.QueueConfig{
			Name:                config.Name,
			Type:                config.Type,
			MaxLength:           int32(config.MaxLength),
			MaxRetries:          int32(config.MaxRetries),
			MessageTtlMs:        config.MessageTTL.Milliseconds(),
			VisibilityTimeoutMs: config.VisibilityTimeout.Milliseconds(),
			DlqEnabled:          config.DLQEnabled,
			DlqName:             config.DLQName,
		},
		Stats: &pb.QueueStats{
			Name:              stats.Name,
			Length:            int32(stats.Length),
			Ready:             int32(stats.Ready),
			Scheduled:         int32(stats.Scheduled),
			DeliveredCount:    stats.DeliveredCount,
			AcknowledgedCount: stats.AcknowledgedCount,
			FailedCount:       stats.FailedCount,
			DlqCount:          int32(stats.DLQCount),
			CreatedAt:         stats.CreatedAt.UnixMilli(),
			UpdatedAt:         stats.UpdatedAt.UnixMilli(),
		},
		Found: true,
	}, nil
}

func (s *QueueGRPCServer) ListQueues(ctx context.Context, req *pb.ListQueuesRequest) (*pb.ListQueuesResponse, error) {
	queues := s.queueManager.ListQueues()

	// Apply prefix filter if provided
	if req.Prefix != "" {
		filtered := make([]string, 0)
		for _, name := range queues {
			if len(name) >= len(req.Prefix) && name[:len(req.Prefix)] == req.Prefix {
				filtered = append(filtered, name)
			}
		}
		queues = filtered
	}

	// Apply limit
	if req.Limit > 0 && int(req.Limit) < len(queues) {
		queues = queues[:req.Limit]
	}

	return &pb.ListQueuesResponse{
		QueueNames: queues,
		Count:      int32(len(queues)),
	}, nil
}

func (s *QueueGRPCServer) GetQueueStats(ctx context.Context, req *pb.GetQueueStatsRequest) (*pb.GetQueueStatsResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	q, err := s.queueManager.GetQueue(req.Name)
	if err != nil {
		return &pb.GetQueueStatsResponse{
			Found: false,
		}, nil
	}

	stats := q.Stats()

	return &pb.GetQueueStatsResponse{
		Stats: &pb.QueueStats{
			Name:              stats.Name,
			Length:            int32(stats.Length),
			Ready:             int32(stats.Ready),
			Scheduled:         int32(stats.Scheduled),
			DeliveredCount:    stats.DeliveredCount,
			AcknowledgedCount: stats.AcknowledgedCount,
			FailedCount:       stats.FailedCount,
			DlqCount:          int32(stats.DLQCount),
			CreatedAt:         stats.CreatedAt.UnixMilli(),
			UpdatedAt:         stats.UpdatedAt.UnixMilli(),
		},
		Found: true,
	}, nil
}

func (s *QueueGRPCServer) PurgeQueue(ctx context.Context, req *pb.PurgeQueueRequest) (*pb.PurgeQueueResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	q, err := s.queueManager.GetQueue(req.QueueName)
	if err != nil {
		return &pb.PurgeQueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	stats := q.Stats()
	messagesDeleted := stats.Length

	if err := s.queueManager.Purge(req.QueueName); err != nil {
		s.logger.Error("Failed to purge queue",
			zap.String("queue", req.QueueName),
			zap.Error(err),
		)
		return &pb.PurgeQueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.PurgeQueueResponse{
		Success:         true,
		MessagesDeleted: int32(messagesDeleted),
	}, nil
}

// Message operations

func (s *QueueGRPCServer) Enqueue(ctx context.Context, req *pb.EnqueueRequest) (*pb.EnqueueResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if req.Body == nil || len(req.Body) == 0 {
		return nil, status.Error(codes.InvalidArgument, "message body is required")
	}

	msg := queue.NewMessage(req.QueueName, req.Body)
	msg.Priority = int(req.Priority)
	if req.Headers != nil {
		msg.Headers = req.Headers
	}
	if req.ScheduledAt > 0 {
		scheduledAt := time.UnixMilli(req.ScheduledAt)
		msg.ScheduledAt = &scheduledAt
	}
	if req.ExpiresAt > 0 {
		expiresAt := time.UnixMilli(req.ExpiresAt)
		msg.ExpiresAt = &expiresAt
	}

	if err := s.queueManager.Enqueue(req.QueueName, msg); err != nil {
		s.logger.Error("Failed to enqueue message",
			zap.String("queue", req.QueueName),
			zap.Error(err),
		)
		return &pb.EnqueueResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.EnqueueResponse{
		Success:   true,
		MessageId: msg.ID,
	}, nil
}

func (s *QueueGRPCServer) Dequeue(ctx context.Context, req *pb.DequeueRequest) (*pb.DequeueResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	msg, err := s.queueManager.Dequeue(req.QueueName)
	if err != nil {
		if err.Error() == "queue is empty" {
			return &pb.DequeueResponse{
				Found: false,
			}, nil
		}
		s.logger.Error("Failed to dequeue message",
			zap.String("queue", req.QueueName),
			zap.Error(err),
		)
		return &pb.DequeueResponse{
			Found: false,
			Error: err.Error(),
		}, nil
	}

	return &pb.DequeueResponse{
		Message: convertMessageToProto(msg),
		Found:   true,
	}, nil
}

func (s *QueueGRPCServer) Peek(ctx context.Context, req *pb.PeekRequest) (*pb.PeekResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	msg, err := s.queueManager.Peek(req.QueueName)
	if err != nil {
		if err.Error() == "queue is empty" {
			return &pb.PeekResponse{
				Found: false,
			}, nil
		}
		s.logger.Error("Failed to peek message",
			zap.String("queue", req.QueueName),
			zap.Error(err),
		)
		return &pb.PeekResponse{
			Found: false,
			Error: err.Error(),
		}, nil
	}

	return &pb.PeekResponse{
		Message: convertMessageToProto(msg),
		Found:   true,
	}, nil
}

func (s *QueueGRPCServer) Acknowledge(ctx context.Context, req *pb.AcknowledgeRequest) (*pb.AcknowledgeResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if req.MessageId == "" {
		return nil, status.Error(codes.InvalidArgument, "message ID is required")
	}

	if err := s.queueManager.Acknowledge(req.QueueName, req.MessageId); err != nil {
		s.logger.Error("Failed to acknowledge message",
			zap.String("queue", req.QueueName),
			zap.String("message_id", req.MessageId),
			zap.Error(err),
		)
		return &pb.AcknowledgeResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.AcknowledgeResponse{
		Success: true,
	}, nil
}

func (s *QueueGRPCServer) Nack(ctx context.Context, req *pb.NackRequest) (*pb.NackResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if req.MessageId == "" {
		return nil, status.Error(codes.InvalidArgument, "message ID is required")
	}

	if err := s.queueManager.Nack(req.QueueName, req.MessageId, req.Requeue); err != nil {
		s.logger.Error("Failed to nack message",
			zap.String("queue", req.QueueName),
			zap.String("message_id", req.MessageId),
			zap.Bool("requeue", req.Requeue),
			zap.Error(err),
		)
		return &pb.NackResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.NackResponse{
		Success: true,
	}, nil
}

func (s *QueueGRPCServer) GetMessage(ctx context.Context, req *pb.GetMessageRequest) (*pb.GetMessageResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if req.MessageId == "" {
		return nil, status.Error(codes.InvalidArgument, "message ID is required")
	}

	msg, err := s.queueManager.Get(req.QueueName, req.MessageId)
	if err != nil {
		return &pb.GetMessageResponse{
			Found: false,
			Error: err.Error(),
		}, nil
	}

	return &pb.GetMessageResponse{
		Message: convertMessageToProto(msg),
		Found:   true,
	}, nil
}

func (s *QueueGRPCServer) DeleteMessage(ctx context.Context, req *pb.DeleteMessageRequest) (*pb.DeleteMessageResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if req.MessageId == "" {
		return nil, status.Error(codes.InvalidArgument, "message ID is required")
	}

	if err := s.queueManager.Delete(req.QueueName, req.MessageId); err != nil {
		s.logger.Error("Failed to delete message",
			zap.String("queue", req.QueueName),
			zap.String("message_id", req.MessageId),
			zap.Error(err),
		)
		return &pb.DeleteMessageResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	return &pb.DeleteMessageResponse{
		Success: true,
	}, nil
}

// Batch operations

func (s *QueueGRPCServer) BatchEnqueue(ctx context.Context, req *pb.BatchEnqueueRequest) (*pb.BatchEnqueueResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if len(req.Messages) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one message is required")
	}

	messages := make([]*queue.Message, 0, len(req.Messages))
	for _, msgReq := range req.Messages {
		msg := queue.NewMessage(req.QueueName, msgReq.Body)
		msg.Priority = int(msgReq.Priority)
		if msgReq.Headers != nil {
			msg.Headers = msgReq.Headers
		}
		if msgReq.ScheduledAt > 0 {
			scheduledAt := time.UnixMilli(msgReq.ScheduledAt)
			msg.ScheduledAt = &scheduledAt
		}
		if msgReq.ExpiresAt > 0 {
			expiresAt := time.UnixMilli(msgReq.ExpiresAt)
			msg.ExpiresAt = &expiresAt
		}
		messages = append(messages, msg)
	}

	errors, err := s.queueManager.BatchEnqueue(req.QueueName, messages)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	successful := int32(0)
	failed := int32(0)
	results := make([]*pb.EnqueueResponse, len(messages))
	for i, msg := range messages {
		if errors[i] == nil {
			successful++
			results[i] = &pb.EnqueueResponse{
				Success:   true,
				MessageId: msg.ID,
			}
		} else {
			failed++
			results[i] = &pb.EnqueueResponse{
				Success: false,
				Error:   errors[i].Error(),
			}
		}
	}

	return &pb.BatchEnqueueResponse{
		Results:    results,
		Successful: successful,
		Failed:     failed,
	}, nil
}

func (s *QueueGRPCServer) BatchDequeue(ctx context.Context, req *pb.BatchDequeueRequest) (*pb.BatchDequeueResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	count := int(req.Count)
	if count <= 0 {
		count = 1
	}
	if count > 100 {
		count = 100 // Limit batch size
	}

	messages, err := s.queueManager.BatchDequeue(req.QueueName, count)
	if err != nil {
		s.logger.Error("Failed to batch dequeue",
			zap.String("queue", req.QueueName),
			zap.Int("count", count),
			zap.Error(err),
		)
		return nil, status.Error(codes.Internal, err.Error())
	}

	pbMessages := make([]*pb.Message, len(messages))
	for i, msg := range messages {
		pbMessages[i] = convertMessageToProto(msg)
	}

	return &pb.BatchDequeueResponse{
		Messages: pbMessages,
		Count:    int32(len(messages)),
	}, nil
}

func (s *QueueGRPCServer) BatchAcknowledge(ctx context.Context, req *pb.BatchAcknowledgeRequest) (*pb.BatchAcknowledgeResponse, error) {
	if req.QueueName == "" {
		return nil, status.Error(codes.InvalidArgument, "queue name is required")
	}

	if len(req.MessageIds) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one message ID is required")
	}

	errors, err := s.queueManager.BatchAcknowledge(req.QueueName, req.MessageIds)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	successful := int32(0)
	failed := int32(0)
	results := make([]*pb.AcknowledgeResponse, len(req.MessageIds))
	for i := range req.MessageIds {
		if errors[i] == nil {
			successful++
			results[i] = &pb.AcknowledgeResponse{
				Success: true,
			}
		} else {
			failed++
			results[i] = &pb.AcknowledgeResponse{
				Success: false,
				Error:   errors[i].Error(),
			}
		}
	}

	return &pb.BatchAcknowledgeResponse{
		Results:    results,
		Successful: successful,
		Failed:     failed,
	}, nil
}

// Helper functions

func convertMessageToProto(msg *queue.Message) *pb.Message {
	pbMsg := &pb.Message{
		Id:            msg.ID,
		QueueName:     msg.QueueName,
		Body:          msg.Body,
		Priority:      int32(msg.Priority),
		Headers:       msg.Headers,
		CreatedAt:     msg.CreatedAt.UnixMilli(),
		DeliveryCount: int32(msg.DeliveryCount),
		DlqReason:     msg.DLQReason,
	}

	if msg.ScheduledAt != nil {
		pbMsg.ScheduledAt = msg.ScheduledAt.UnixMilli()
	}

	if msg.ExpiresAt != nil {
		pbMsg.ExpiresAt = msg.ExpiresAt.UnixMilli()
	}

	if msg.LastDelivered != nil {
		pbMsg.LastDelivered = msg.LastDelivered.UnixMilli()
	}

	return pbMsg
}
