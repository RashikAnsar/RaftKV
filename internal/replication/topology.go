package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/RashikAnsar/raftkv/api/proto"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TopologyManager manages datacenter topology and health
type TopologyManager struct {
	mu           sync.RWMutex
	config       Config
	datacenters  map[string]*Datacenter
	logger       *zap.Logger
	healthTicker *time.Ticker
	stopCh       chan struct{}
}

// Datacenter represents a remote datacenter
type Datacenter struct {
	ID        string
	Name      string
	Endpoints []string
	Region    string
	Priority  int

	// Connection state
	mu          sync.RWMutex
	conn        *grpc.ClientConn
	client      pb.ReplicationServiceClient
	isHealthy   bool
	lastHealthy time.Time
	lastError   error
	failures    int
}

// NewTopologyManager creates a new topology manager
func NewTopologyManager(config Config, logger *zap.Logger) *TopologyManager {
	return &TopologyManager{
		config:      config,
		datacenters: make(map[string]*Datacenter),
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// Start initializes connections to all target datacenters
func (tm *TopologyManager) Start(ctx context.Context) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Initialize datacenters from config
	for _, target := range tm.config.Targets {
		dc := &Datacenter{
			ID:        target.ID,
			Name:      target.Name,
			Endpoints: target.Endpoints,
			Region:    target.Region,
			Priority:  target.Priority,
			isHealthy: false,
		}

		// Establish connection
		if err := tm.connectDatacenter(ctx, dc, target); err != nil {
			tm.logger.Warn("Failed to connect to datacenter",
				zap.String("dc_id", dc.ID),
				zap.Error(err),
			)
			// Continue to next DC, don't fail startup
		}

		tm.datacenters[dc.ID] = dc
	}

	// Start health checking if enabled
	if tm.config.HealthCheck.Enabled {
		tm.startHealthChecking()
	}

	tm.logger.Info("Topology manager started",
		zap.Int("datacenters", len(tm.datacenters)),
	)

	return nil
}

// connectDatacenter establishes a gRPC connection to a datacenter
func (tm *TopologyManager) connectDatacenter(ctx context.Context, dc *Datacenter, target DatacenterTarget) error {
	// Try each endpoint until one succeeds
	var lastErr error
	for _, endpoint := range target.Endpoints {
		// TODO: Add TLS support
		opts := []grpc.DialOption{
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithBlock(),
		}

		// Set connection timeout
		connCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		conn, err := grpc.DialContext(connCtx, endpoint, opts...)
		if err != nil {
			lastErr = err
			tm.logger.Debug("Failed to connect to endpoint",
				zap.String("dc_id", dc.ID),
				zap.String("endpoint", endpoint),
				zap.Error(err),
			)
			continue
		}

		// Create client
		client := pb.NewReplicationServiceClient(conn)

		// Test connection with heartbeat
		hbCtx, hbCancel := context.WithTimeout(ctx, 2*time.Second)
		_, err = client.Heartbeat(hbCtx, &pb.HeartbeatRequest{
			DatacenterId: tm.config.DatacenterID,
			Timestamp:    nil, // Will be set by protobuf
		})
		hbCancel()

		if err != nil {
			conn.Close()
			lastErr = err
			continue
		}

		// Success!
		dc.mu.Lock()
		dc.conn = conn
		dc.client = client
		dc.isHealthy = true
		dc.lastHealthy = time.Now()
		dc.failures = 0
		dc.mu.Unlock()

		tm.logger.Info("Connected to datacenter",
			zap.String("dc_id", dc.ID),
			zap.String("endpoint", endpoint),
		)

		return nil
	}

	return fmt.Errorf("failed to connect to any endpoint: %w", lastErr)
}

// startHealthChecking starts periodic health checks
func (tm *TopologyManager) startHealthChecking() {
	tm.healthTicker = time.NewTicker(tm.config.HealthCheck.Interval)

	go func() {
		for {
			select {
			case <-tm.healthTicker.C:
				tm.performHealthChecks()
			case <-tm.stopCh:
				return
			}
		}
	}()

	tm.logger.Info("Health checking started",
		zap.Duration("interval", tm.config.HealthCheck.Interval),
	)
}

// performHealthChecks checks health of all datacenters
func (tm *TopologyManager) performHealthChecks() {
	tm.mu.RLock()
	datacenters := make([]*Datacenter, 0, len(tm.datacenters))
	for _, dc := range tm.datacenters {
		datacenters = append(datacenters, dc)
	}
	tm.mu.RUnlock()

	for _, dc := range datacenters {
		go tm.checkDatacenterHealth(dc)
	}
}

// checkDatacenterHealth performs a health check on a single datacenter
func (tm *TopologyManager) checkDatacenterHealth(dc *Datacenter) {
	dc.mu.RLock()
	client := dc.client
	dc.mu.RUnlock()

	if client == nil {
		tm.markUnhealthy(dc, fmt.Errorf("no connection"))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), tm.config.HealthCheck.Timeout)
	defer cancel()

	resp, err := client.Heartbeat(ctx, &pb.HeartbeatRequest{
		DatacenterId: tm.config.DatacenterID,
	})

	if err != nil {
		tm.markUnhealthy(dc, err)
		return
	}

	// Check health status in response
	if resp.Health != pb.HealthStatus_HEALTH_HEALTHY {
		tm.markUnhealthy(dc, fmt.Errorf("remote datacenter unhealthy: %v", resp.Health))
		return
	}

	// Mark as healthy
	dc.mu.Lock()
	dc.isHealthy = true
	dc.lastHealthy = time.Now()
	dc.lastError = nil
	dc.failures = 0
	dc.mu.Unlock()
}

// markUnhealthy marks a datacenter as unhealthy
func (tm *TopologyManager) markUnhealthy(dc *Datacenter, err error) {
	dc.mu.Lock()
	dc.lastError = err
	dc.failures++

	if dc.failures >= tm.config.HealthCheck.FailureThreshold {
		if dc.isHealthy {
			tm.logger.Warn("Datacenter marked unhealthy",
				zap.String("dc_id", dc.ID),
				zap.Int("failures", dc.failures),
				zap.Error(err),
			)
		}
		dc.isHealthy = false
	}
	dc.mu.Unlock()
}

// GetDatacenter returns a datacenter by ID
func (tm *TopologyManager) GetDatacenter(id string) (*Datacenter, error) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	dc, exists := tm.datacenters[id]
	if !exists {
		return nil, fmt.Errorf("datacenter not found: %s", id)
	}

	return dc, nil
}

// GetHealthyDatacenters returns all healthy datacenters
func (tm *TopologyManager) GetHealthyDatacenters() []*Datacenter {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	healthy := make([]*Datacenter, 0)
	for _, dc := range tm.datacenters {
		dc.mu.RLock()
		if dc.isHealthy {
			healthy = append(healthy, dc)
		}
		dc.mu.RUnlock()
	}

	return healthy
}

// GetAllDatacenters returns all datacenters
func (tm *TopologyManager) GetAllDatacenters() []*Datacenter {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	all := make([]*Datacenter, 0, len(tm.datacenters))
	for _, dc := range tm.datacenters {
		all = append(all, dc)
	}

	return all
}

// Stop stops the topology manager
func (tm *TopologyManager) Stop() error {
	// Stop health checking
	if tm.healthTicker != nil {
		tm.healthTicker.Stop()
	}
	close(tm.stopCh)

	// Close all connections
	tm.mu.Lock()
	defer tm.mu.Unlock()

	for id, dc := range tm.datacenters {
		dc.mu.Lock()
		if dc.conn != nil {
			if err := dc.conn.Close(); err != nil {
				tm.logger.Warn("Error closing connection",
					zap.String("dc_id", id),
					zap.Error(err),
				)
			}
		}
		dc.mu.Unlock()
	}

	tm.logger.Info("Topology manager stopped")
	return nil
}

// GetClient returns the gRPC client for a datacenter
func (dc *Datacenter) GetClient() pb.ReplicationServiceClient {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.client
}

// IsHealthy returns true if the datacenter is healthy
func (dc *Datacenter) IsHealthy() bool {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.isHealthy
}

// GetLastError returns the last error encountered
func (dc *Datacenter) GetLastError() error {
	dc.mu.RLock()
	defer dc.mu.RUnlock()
	return dc.lastError
}

// GetStats returns datacenter statistics
func (dc *Datacenter) GetStats() DatacenterStats {
	dc.mu.RLock()
	defer dc.mu.RUnlock()

	return DatacenterStats{
		ID:          dc.ID,
		Name:        dc.Name,
		Region:      dc.Region,
		IsHealthy:   dc.isHealthy,
		LastHealthy: dc.lastHealthy,
		Failures:    dc.failures,
		LastError:   dc.lastError,
	}
}

// DatacenterStats contains datacenter statistics
type DatacenterStats struct {
	ID          string
	Name        string
	Region      string
	IsHealthy   bool
	LastHealthy time.Time
	Failures    int
	LastError   error
}
