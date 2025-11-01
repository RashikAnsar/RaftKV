package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/RashikAnsar/raftkv/api/proto"
)

// GRPCClient is a high-performance gRPC client for the KV store
type GRPCClient struct {
	// Connection pool
	conns map[string]*grpc.ClientConn
	mu    sync.RWMutex

	// Known servers
	servers []string

	// Current leader (cached)
	leaderAddr string
	leaderMu   sync.RWMutex

	// Configuration
	timeout         time.Duration
	maxRetries      int
	retryBackoff    time.Duration
	enableAutoRetry bool
}

// GRPCClientConfig holds configuration for the gRPC client
type GRPCClientConfig struct {
	// Initial list of server addresses
	Servers []string

	// Request timeout (default: 5s)
	Timeout time.Duration

	// Max retries on failure (default: 3)
	MaxRetries int

	// Retry backoff duration (default: 100ms)
	RetryBackoff time.Duration

	// Automatically retry on leader redirect (default: true)
	EnableAutoRetry bool
}

// NewGRPCClient creates a new gRPC client
func NewGRPCClient(config GRPCClientConfig) (*GRPCClient, error) {
	if len(config.Servers) == 0 {
		return nil, fmt.Errorf("at least one server address is required")
	}

	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = 5 * time.Second
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}
	if config.RetryBackoff == 0 {
		config.RetryBackoff = 100 * time.Millisecond
	}

	client := &GRPCClient{
		conns:           make(map[string]*grpc.ClientConn),
		servers:         config.Servers,
		timeout:         config.Timeout,
		maxRetries:      config.MaxRetries,
		retryBackoff:    config.RetryBackoff,
		enableAutoRetry: config.EnableAutoRetry,
	}

	return client, nil
}

// getConnection gets or creates a connection to a server
func (c *GRPCClient) getConnection(addr string) (*grpc.ClientConn, error) {
	c.mu.RLock()
	conn, exists := c.conns[addr]
	c.mu.RUnlock()

	if exists && conn.GetState().String() != "SHUTDOWN" {
		return conn, nil
	}

	// Create new connection
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if conn, exists := c.conns[addr]; exists && conn.GetState().String() != "SHUTDOWN" {
		return conn, nil
	}

	// Create connection with keepalive
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithTimeout(c.timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", addr, err)
	}

	c.conns[addr] = conn
	return conn, nil
}

// getClient gets a KVStore client for the given address
func (c *GRPCClient) getClient(addr string) (pb.KVStoreClient, error) {
	conn, err := c.getConnection(addr)
	if err != nil {
		return nil, err
	}
	return pb.NewKVStoreClient(conn), nil
}

// discoverLeader attempts to discover the current leader
func (c *GRPCClient) discoverLeader(ctx context.Context) (string, error) {
	// Try each known server
	for _, addr := range c.servers {
		client, err := c.getClient(addr)
		if err != nil {
			continue
		}

		resp, err := client.GetLeader(ctx, &pb.LeaderRequest{})
		if err != nil {
			continue
		}

		// If this server is the leader, use its address
		if resp.IsLeader {
			c.leaderMu.Lock()
			c.leaderAddr = addr // Use the server we connected to
			c.leaderMu.Unlock()
			return addr, nil
		}

		// If this server knows the leader's address, use it
		if resp.LeaderAddress != "" {
			c.leaderMu.Lock()
			c.leaderAddr = resp.LeaderAddress
			c.leaderMu.Unlock()
			return resp.LeaderAddress, nil
		}
	}

	return "", fmt.Errorf("failed to discover leader")
}

// getLeaderClient gets a client connected to the current leader
func (c *GRPCClient) getLeaderClient(ctx context.Context) (pb.KVStoreClient, error) {
	// Check cached leader
	c.leaderMu.RLock()
	leaderAddr := c.leaderAddr
	c.leaderMu.RUnlock()

	if leaderAddr != "" {
		client, err := c.getClient(leaderAddr)
		if err == nil {
			return client, nil
		}
	}

	// If auto-retry is disabled, try the first configured server directly
	// This is useful for CLI operations where we don't need automatic failover
	if !c.enableAutoRetry && len(c.servers) > 0 {
		// Try connecting directly to the first server
		client, err := c.getClient(c.servers[0])
		if err == nil {
			// Cache this as the leader for subsequent requests
			c.leaderMu.Lock()
			c.leaderAddr = c.servers[0]
			c.leaderMu.Unlock()
			return client, nil
		}
		return nil, fmt.Errorf("failed to connect to server %s: %w", c.servers[0], err)
	}

	// Discover leader (only if auto-retry is enabled)
	leaderAddr, err := c.discoverLeader(ctx)
	if err != nil {
		return nil, err
	}

	return c.getClient(leaderAddr)
}

// Get retrieves a value for a given key
func (c *GRPCClient) Get(ctx context.Context, key string, consistent bool) ([]byte, bool, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// For consistent reads, must go to leader
	var client pb.KVStoreClient
	var err error

	if consistent {
		client, err = c.getLeaderClient(reqCtx)
	} else {
		// For non-consistent reads, try any server
		for _, addr := range c.servers {
			client, err = c.getClient(addr)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return nil, false, fmt.Errorf("failed to get client: %w", err)
	}

	resp, err := client.Get(reqCtx, &pb.GetRequest{
		Key:        key,
		Consistent: consistent,
	})
	if err != nil {
		return nil, false, fmt.Errorf("get failed: %w", err)
	}

	return resp.Value, resp.Found, nil
}

// Put stores a key-value pair
func (c *GRPCClient) Put(ctx context.Context, key string, value []byte) error {
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(c.retryBackoff * time.Duration(attempt))
		}

		reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		// Get leader client
		client, err := c.getLeaderClient(reqCtx)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := client.Put(reqCtx, &pb.PutRequest{
			Key:   key,
			Value: value,
		})

		if err != nil {
			// Check if it's a "not leader" error
			if st, ok := status.FromError(err); ok {
				if st.Code() == codes.FailedPrecondition {
					// Leader changed, clear cache and retry
					c.leaderMu.Lock()
					c.leaderAddr = ""
					c.leaderMu.Unlock()

					if c.enableAutoRetry {
						lastErr = err
						continue
					}
				}
			}
			return fmt.Errorf("put failed: %w", err)
		}

		if !resp.Success {
			return fmt.Errorf("put failed: %s", resp.Error)
		}

		return nil
	}

	return fmt.Errorf("put failed after %d attempts: %w", c.maxRetries, lastErr)
}

// Delete removes a key-value pair
func (c *GRPCClient) Delete(ctx context.Context, key string) error {
	var lastErr error

	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(c.retryBackoff * time.Duration(attempt))
		}

		reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		// Get leader client
		client, err := c.getLeaderClient(reqCtx)
		if err != nil {
			lastErr = err
			continue
		}

		resp, err := client.Delete(reqCtx, &pb.DeleteRequest{
			Key: key,
		})

		if err != nil {
			// Check if it's a "not leader" error
			if st, ok := status.FromError(err); ok {
				if st.Code() == codes.FailedPrecondition {
					// Leader changed, clear cache and retry
					c.leaderMu.Lock()
					c.leaderAddr = ""
					c.leaderMu.Unlock()

					if c.enableAutoRetry {
						lastErr = err
						continue
					}
				}
			}
			return fmt.Errorf("delete failed: %w", err)
		}

		if !resp.Success {
			return fmt.Errorf("delete failed: %s", resp.Error)
		}

		return nil
	}

	return fmt.Errorf("delete failed after %d attempts: %w", c.maxRetries, lastErr)
}

// List returns keys matching a prefix
func (c *GRPCClient) List(ctx context.Context, prefix string, limit int) ([]string, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Try any server for reads
	var client pb.KVStoreClient
	var err error

	for _, addr := range c.servers {
		client, err = c.getClient(addr)
		if err == nil {
			break
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	resp, err := client.List(reqCtx, &pb.ListRequest{
		Prefix: prefix,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list failed: %w", err)
	}

	return resp.Keys, nil
}

// GetStats returns store statistics
func (c *GRPCClient) GetStats(ctx context.Context) (*pb.StatsResponse, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	// Try leader first
	client, err := c.getLeaderClient(reqCtx)
	if err != nil {
		// Fallback to any server
		for _, addr := range c.servers {
			client, err = c.getClient(addr)
			if err == nil {
				break
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	resp, err := client.GetStats(reqCtx, &pb.StatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("get stats failed: %w", err)
	}

	return resp, nil
}

// GetLeader returns the current leader information
func (c *GRPCClient) GetLeader(ctx context.Context) (string, error) {
	leaderAddr, err := c.discoverLeader(ctx)
	if err != nil {
		return "", err
	}
	return leaderAddr, nil
}

// Close closes all connections
func (c *GRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var errs []error
	for addr, conn := range c.conns {
		if err := conn.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close connection to %s: %w", addr, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing connections: %v", errs)
	}

	return nil
}
