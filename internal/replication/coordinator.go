package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"github.com/RashikAnsar/raftkv/internal/cdc"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Coordinator orchestrates replication to multiple datacenters
// Manages CDC subscriptions, streaming connections, and replication workers
type Coordinator struct {
	config       Config
	publisher    *cdc.Publisher
	topology     *TopologyManager
	lagMonitor   *LagMonitor
	logger       *zap.Logger

	mu      sync.RWMutex
	workers map[string]*replicationWorker // Map of DC ID -> worker
	running bool
	stopCh  chan struct{}
}

// replicationWorker handles replication to a single datacenter
type replicationWorker struct {
	datacenterID   string
	datacenter     *Datacenter
	subscriber     *cdc.Subscriber
	coordinator    *Coordinator
	logger         *zap.Logger
	stopCh         chan struct{}
	lastReplicated uint64
	mu             sync.RWMutex
}

// NewCoordinator creates a new replication coordinator
func NewCoordinator(config Config, publisher *cdc.Publisher, topology *TopologyManager, logger *zap.Logger) *Coordinator {
	lagMonitor := NewLagMonitor(config.DatacenterID, logger)

	return &Coordinator{
		config:     config,
		publisher:  publisher,
		topology:   topology,
		lagMonitor: lagMonitor,
		logger:     logger,
		workers:    make(map[string]*replicationWorker),
		stopCh:     make(chan struct{}),
	}
}

// Start starts the replication coordinator
func (c *Coordinator) Start(ctx context.Context) error {
	if !c.config.Enabled {
		c.logger.Info("Replication is disabled")
		return nil
	}

	if !c.config.IsPrimary() {
		c.logger.Info("This datacenter is a replica, not starting outbound replication")
		return nil
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("coordinator already running")
	}
	c.running = true
	c.mu.Unlock()

	// Start replication workers for each target datacenter
	for _, target := range c.config.Targets {
		dc, err := c.topology.GetDatacenter(target.ID)
		if err != nil {
			c.logger.Warn("Failed to get datacenter, skipping",
				zap.String("dc_id", target.ID),
				zap.Error(err),
			)
			continue
		}

		worker := c.createWorker(dc)
		c.mu.Lock()
		c.workers[target.ID] = worker
		c.mu.Unlock()

		go worker.run()
	}

	c.logger.Info("Replication coordinator started",
		zap.Int("workers", len(c.workers)),
		zap.String("datacenter_id", c.config.DatacenterID),
	)

	return nil
}

// createWorker creates a replication worker for a datacenter
func (c *Coordinator) createWorker(dc *Datacenter) *replicationWorker {
	subscriber := c.publisher.Subscribe(
		fmt.Sprintf("replication-%s", dc.ID),
		c.config.BufferSize,
		nil, // No filter, replicate everything
	)

	return &replicationWorker{
		datacenterID: dc.ID,
		datacenter:   dc,
		subscriber:   subscriber,
		coordinator:  c,
		logger:       c.logger.With(zap.String("target_dc", dc.ID)),
		stopCh:       make(chan struct{}),
	}
}

// Stop stops the replication coordinator
func (c *Coordinator) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	c.mu.Unlock()

	close(c.stopCh)

	// Stop all workers
	c.mu.RLock()
	workers := make([]*replicationWorker, 0, len(c.workers))
	for _, w := range c.workers {
		workers = append(workers, w)
	}
	c.mu.RUnlock()

	for _, w := range workers {
		w.stop()
	}

	c.logger.Info("Replication coordinator stopped")
	return nil
}

// GetStatus returns the current replication status
func (c *Coordinator) GetStatus() *pb.ReplicationStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status := &pb.ReplicationStatus{
		DatacenterId: c.config.DatacenterID,
		Health:       pb.HealthStatus_HEALTH_HEALTHY,
		LagByDc:      make(map[string]*pb.DatacenterLag),
	}

	// Collect lag info from each worker
	for dcID, worker := range c.workers {
		lag := c.lagMonitor.GetLag(dcID)
		status.LagByDc[dcID] = &pb.DatacenterLag{
			DatacenterId:        dcID,
			LastReplicatedIndex: worker.getLastReplicated(),
			LagEntries:          lag.LagEntries,
			LagSeconds:          lag.LagSeconds,
			Connected:           worker.datacenter.IsHealthy(),
			LastSuccess:         timestamppb.New(lag.LastSuccess),
		}
	}

	// Overall health based on worker health
	unhealthyCount := 0
	for _, dc := range c.workers {
		if !dc.datacenter.IsHealthy() {
			unhealthyCount++
		}
	}

	if unhealthyCount == len(c.workers) && len(c.workers) > 0 {
		status.Health = pb.HealthStatus_HEALTH_UNHEALTHY
	} else if unhealthyCount > 0 {
		status.Health = pb.HealthStatus_HEALTH_DEGRADED
	}

	return status
}

// replicationWorker.run runs the replication loop for a datacenter
func (w *replicationWorker) run() {
	w.logger.Info("Replication worker started")

	// Retry loop with exponential backoff
	backoff := w.coordinator.config.Retry.InitialBackoff
	retries := 0

	for {
		select {
		case <-w.stopCh:
			w.logger.Info("Replication worker stopped")
			return
		default:
			// Check if datacenter is healthy
			if !w.datacenter.IsHealthy() {
				w.logger.Debug("Datacenter unhealthy, waiting before retry")
				time.Sleep(backoff)
				continue
			}

			// Attempt to replicate
			err := w.replicate()
			if err != nil {
				w.logger.Error("Replication error", zap.Error(err))

				// Exponential backoff
				retries++
				if retries >= w.coordinator.config.Retry.MaxRetries {
					w.logger.Error("Max retries exceeded, resetting backoff",
						zap.Int("retries", retries),
					)
					retries = 0
				}

				backoff = time.Duration(float64(w.coordinator.config.Retry.InitialBackoff) *
					float64(retries) * w.coordinator.config.Retry.BackoffMultiplier)
				if backoff > w.coordinator.config.Retry.MaxBackoff {
					backoff = w.coordinator.config.Retry.MaxBackoff
				}

				time.Sleep(backoff)
				continue
			}

			// Reset on success
			retries = 0
			backoff = w.coordinator.config.Retry.InitialBackoff
		}
	}
}

// replicate performs the actual replication streaming
func (w *replicationWorker) replicate() error {
	client := w.datacenter.GetClient()
	if client == nil {
		return fmt.Errorf("no client available")
	}

	// Create stream request
	req := &pb.StreamRequest{
		FromIndex:    w.getLastReplicated(),
		DatacenterId: w.coordinator.config.DatacenterID,
		BatchSize:    int32(w.coordinator.config.BatchSize),
	}

	w.logger.Info("Starting replication stream",
		zap.Uint64("from_index", req.FromIndex),
	)

	// Stream events from local subscriber to remote datacenter
	// (In production, this would use the gRPC stream to send events)
	eventCount := uint64(0)
	for {
		select {
		case <-w.stopCh:
			return nil
		default:
			// Receive event from subscriber
			event, err := w.subscriber.RecvWithTimeout(1 * time.Second)
			if err == context.DeadlineExceeded {
				// No event available, continue
				continue
			}
			if err != nil {
				return fmt.Errorf("failed to receive event: %w", err)
			}

			// TODO: Send event to remote datacenter via gRPC stream
			// For now, just track that we would replicate it

			// Update metrics
			w.setLastReplicated(event.RaftIndex)
			w.coordinator.lagMonitor.UpdateLag(w.datacenterID, event.RaftIndex, time.Now())

			eventCount++
			if eventCount%100 == 0 {
				w.logger.Debug("Replication progress",
					zap.Uint64("events", eventCount),
					zap.Uint64("last_index", event.RaftIndex),
				)
			}
		}
	}
}

// stop stops the replication worker
func (w *replicationWorker) stop() {
	close(w.stopCh)
	w.coordinator.publisher.Unsubscribe(w.subscriber.ID())
}

// getLastReplicated returns the last replicated index
func (w *replicationWorker) getLastReplicated() uint64 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.lastReplicated
}

// setLastReplicated sets the last replicated index
func (w *replicationWorker) setLastReplicated(index uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if index > w.lastReplicated {
		w.lastReplicated = index
	}
}
