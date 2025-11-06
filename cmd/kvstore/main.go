package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

type Config struct {
	// Server config
	HTTPAddr   string
	GRPCAddr   string
	EnableGRPC bool

	// Storage config
	DataDir       string
	SyncOnWrite   bool
	SnapshotEvery int

	// Raft config
	EnableRaft bool
	NodeID     string
	RaftAddr   string
	RaftDir    string
	Bootstrap  bool
	JoinAddr   string

	// Observability
	LogLevel string

	// Performance
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRequestSize  int64
	EnableRateLimit bool
	RateLimit       int

	// Cache config
	EnableCache   bool
	CacheSize     int
	CacheTTL      time.Duration
}

func main() {
	// Parse command-line flags
	config := parseFlags()

	// Initialize logger
	logger, err := observability.NewLogger(config.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	observability.SetGlobalLogger(logger)

	logger.Info("Starting RaftKV",
		zap.String("version", "1.0.0"),
		zap.String("http_addr", config.HTTPAddr),
		zap.String("grpc_addr", config.GRPCAddr),
		zap.Bool("grpc_enabled", config.EnableGRPC),
		zap.String("data_dir", config.DataDir),
		zap.Bool("raft_enabled", config.EnableRaft),
	)

	// Create storage - ALWAYS use DurableStore (unified architecture)
	durableStore, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       config.DataDir,
		SyncOnWrite:   config.SyncOnWrite,
		SnapshotEvery: config.SnapshotEvery,
	})
	if err != nil {
		logger.Fatal("Failed to create store", zap.Error(err))
	}
	defer durableStore.Close()

	var store storage.Store = durableStore

	// Wrap with cache if enabled
	if config.EnableCache {
		store = storage.NewCachedStore(durableStore, storage.CacheConfig{
			MaxSize: config.CacheSize,
			TTL:     config.CacheTTL,
		})
		logger.Info("Read cache enabled",
			zap.Int("cache_size", config.CacheSize),
			zap.Duration("cache_ttl", config.CacheTTL),
		)
	}

	if config.EnableRaft {
		logger.Info("Storage initialized with DurableStore (Raft cluster mode)",
			zap.String("data_dir", config.DataDir),
			zap.Bool("sync_on_write", config.SyncOnWrite),
			zap.Bool("cache_enabled", config.EnableCache),
		)
	} else {
		logger.Info("Storage initialized with DurableStore (single-node mode)",
			zap.String("data_dir", config.DataDir),
			zap.Bool("sync_on_write", config.SyncOnWrite),
			zap.Bool("cache_enabled", config.EnableCache),
		)
	}

	// Create Raft node if enabled
	var raftNode *consensus.RaftNode
	if config.EnableRaft {
		raftNode, err = consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    config.NodeID,
			RaftAddr:  config.RaftAddr,
			RaftDir:   config.RaftDir,
			Bootstrap: config.Bootstrap,
			JoinAddr:  config.JoinAddr,
			Store:     durableStore, // Pass concrete DurableStore type
			Logger:    logger.Logger,
		})
		if err != nil {
			logger.Fatal("Failed to create Raft node", zap.Error(err))
		}
		defer raftNode.Shutdown()

		logger.Info("Raft node created",
			zap.String("node_id", config.NodeID),
			zap.String("raft_addr", config.RaftAddr),
			zap.Bool("bootstrap", config.Bootstrap),
		)

		// Wait for leader election
		if err := raftNode.WaitForLeader(30 * time.Second); err != nil {
			logger.Fatal("Failed to elect leader", zap.Error(err))
		}

		leaderAddr, _ := raftNode.GetLeader()
		logger.Info("Raft cluster ready",
			zap.String("state", raftNode.GetState()),
			zap.String("leader", leaderAddr),
		)
	}

	// Create metrics
	metrics := observability.NewMetrics()

	// Create servers
	if config.EnableRaft {
		// Use Raft-aware HTTP server
		raftHTTPServer := server.NewRaftHTTPServer(server.RaftHTTPServerConfig{
			Addr:            config.HTTPAddr,
			RaftNode:        raftNode,
			Logger:          logger,
			Metrics:         metrics,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
			MaxRequestSize:  config.MaxRequestSize,
			EnableRateLimit: config.EnableRateLimit,
			RateLimit:       config.RateLimit,
		})

		// Start HTTP server in goroutine
		go func() {
			if err := raftHTTPServer.Start(); err != nil {
				logger.Fatal("Raft HTTP server failed", zap.Error(err))
			}
		}()

		logger.Info("Raft HTTP server started", zap.String("addr", config.HTTPAddr))

		// Start gRPC server if enabled
		var grpcServer *server.GRPCServer
		if config.EnableGRPC {
			grpcServer = server.NewGRPCServer(server.GRPCServerConfig{
				Addr:     config.GRPCAddr,
				RaftNode: raftNode,
				Logger:   logger.Logger,
				Metrics:  metrics,
			})

			go func() {
				if err := grpcServer.Start(); err != nil {
					logger.Fatal("gRPC server failed", zap.Error(err))
				}
			}()

			logger.Info("gRPC server started", zap.String("addr", config.GRPCAddr))
		}

		logger.Info("RaftKV is ready to accept requests (cluster mode)",
			zap.Bool("grpc_enabled", config.EnableGRPC),
		)

		// Wait for interrupt signal
		waitForShutdownRaft(logger, raftHTTPServer, raftNode, grpcServer)
	} else {
		// Use standard HTTP server (single-node mode)
		httpServer := server.NewHTTPServer(server.HTTPServerConfig{
			Addr:            config.HTTPAddr,
			Store:           store,
			Logger:          logger,
			Metrics:         metrics,
			ReadTimeout:     config.ReadTimeout,
			WriteTimeout:    config.WriteTimeout,
			MaxRequestSize:  config.MaxRequestSize,
			EnableRateLimit: config.EnableRateLimit,
			RateLimit:       config.RateLimit,
		})

		// Start server in goroutine
		go func() {
			if err := httpServer.Start(); err != nil {
				logger.Fatal("HTTP server failed", zap.Error(err))
			}
		}()

		logger.Info("HTTP server started", zap.String("addr", config.HTTPAddr))
		logger.Info("RaftKV is ready to accept requests (single-node mode)")

		// Wait for interrupt signal
		waitForShutdown(logger, httpServer)
	}
}

func parseFlags() Config {
	config := Config{}

	// Server flags
	flag.StringVar(&config.HTTPAddr, "http-addr", ":8080", "HTTP server address")
	flag.StringVar(&config.GRPCAddr, "grpc-addr", ":9090", "gRPC server address")
	flag.BoolVar(&config.EnableGRPC, "grpc", false, "Enable gRPC server")

	// Storage flags
	flag.StringVar(&config.DataDir, "data-dir", "./data", "Data directory")
	flag.BoolVar(&config.SyncOnWrite, "sync-on-write", true, "Sync writes to disk (durable but slower)")
	flag.IntVar(&config.SnapshotEvery, "snapshot-every", 10000, "Snapshot every N operations")

	// Raft flags
	flag.BoolVar(&config.EnableRaft, "raft", false, "Enable Raft consensus")
	flag.StringVar(&config.NodeID, "node-id", "node1", "Unique node ID")
	flag.StringVar(&config.RaftAddr, "raft-addr", "localhost:7000", "Raft bind address")
	flag.StringVar(&config.RaftDir, "raft-dir", "./data/raft", "Raft data directory")
	flag.BoolVar(&config.Bootstrap, "bootstrap", false, "Bootstrap new cluster")
	flag.StringVar(&config.JoinAddr, "join", "", "Join existing cluster at this address")

	// Observability flags
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// Performance flags
	readTimeoutSec := flag.Int("read-timeout", 10, "Read timeout in seconds")
	writeTimeoutSec := flag.Int("write-timeout", 10, "Write timeout in seconds")
	flag.Int64Var(&config.MaxRequestSize, "max-request-size", 1024*1024, "Max request body size in bytes")
	flag.BoolVar(&config.EnableRateLimit, "enable-rate-limit", false, "Enable rate limiting")
	flag.IntVar(&config.RateLimit, "rate-limit", 1000, "Requests per second limit")

	// Cache flags
	flag.BoolVar(&config.EnableCache, "enable-cache", true, "Enable read cache")
	flag.IntVar(&config.CacheSize, "cache-size", 10000, "Max number of entries in cache")
	cacheTTLSec := flag.Int("cache-ttl", 0, "Cache TTL in seconds (0 = no expiration)")

	flag.Parse()

	config.ReadTimeout = time.Duration(*readTimeoutSec) * time.Second
	config.WriteTimeout = time.Duration(*writeTimeoutSec) * time.Second
	config.CacheTTL = time.Duration(*cacheTTLSec) * time.Second

	return config
}

func waitForShutdown(logger *observability.Logger, httpServer *server.HTTPServer) {
	// Create signal channel
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	logger.Info("Shutting down HTTP server...")
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	logger.Info("Shutdown complete")
}

func waitForShutdownRaft(logger *observability.Logger, raftHTTPServer *server.RaftHTTPServer, raftNode *consensus.RaftNode, grpcServer *server.GRPCServer) {
	// Create signal channel
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for signal
	sig := <-sigChan
	logger.Info("Received shutdown signal", zap.String("signal", sig.String()))

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	logger.Info("Shutting down Raft HTTP server...")
	if err := raftHTTPServer.Shutdown(ctx); err != nil {
		logger.Error("Raft HTTP server shutdown error", zap.Error(err))
	}

	// Shutdown gRPC server if running
	if grpcServer != nil {
		logger.Info("Shutting down gRPC server...")
		grpcServer.Stop()
	}

	// Shutdown Raft node
	logger.Info("Shutting down Raft node...")
	if err := raftNode.Shutdown(); err != nil {
		logger.Error("Raft shutdown error", zap.Error(err))
	}

	logger.Info("Shutdown complete")
}
