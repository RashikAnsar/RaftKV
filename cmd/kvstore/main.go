package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	flag "github.com/spf13/pflag"
	"go.uber.org/zap"

	"github.com/RashikAnsar/raftkv/internal/auth"
	"github.com/RashikAnsar/raftkv/internal/config"
	"github.com/RashikAnsar/raftkv/internal/consensus"
	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/security"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

func main() {
	// Setup flags
	fs := flag.NewFlagSet("kvstore", flag.ExitOnError)
	configFile := fs.String("config", "", "Path to configuration file (YAML or JSON)")

	// Register all configuration flags
	config.RegisterFlags(fs)

	// Parse flags
	fs.Parse(os.Args[1:])

	// Load configuration from multiple sources (flags, env, config file, defaults)
	cfg, err := config.Load(config.LoadOptions{
		ConfigFile: *configFile,
		Flags:      fs,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger, err := observability.NewLogger(cfg.Observability.LogLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	observability.SetGlobalLogger(logger)

	logger.Info("Starting RaftKV",
		zap.String("version", Version),
		zap.String("build_time", BuildTime),
		zap.String("git_commit", GitCommit),
		zap.String("http_addr", cfg.Server.HTTPAddr),
		zap.String("grpc_addr", cfg.Server.GRPCAddr),
		zap.Bool("grpc_enabled", cfg.Server.EnableGRPC),
		zap.String("data_dir", cfg.Storage.DataDir),
		zap.Bool("raft_enabled", cfg.Raft.Enabled),
		zap.Bool("auth_enabled", cfg.Auth.Enabled),
		zap.Bool("tls_enabled", cfg.TLS.Enabled),
		zap.String("config_file", *configFile),
	)

	// Create storage - ALWAYS use DurableStore (unified architecture)
	durableStore, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       cfg.Storage.DataDir,
		SyncOnWrite:   cfg.Storage.SyncOnWrite,
		SnapshotEvery: cfg.Storage.SnapshotEvery,
	})
	if err != nil {
		logger.Fatal("Failed to create store", zap.Error(err))
	}
	defer durableStore.Close()

	var store storage.Store = durableStore

	// Wrap with cache if enabled
	if cfg.Cache.Enabled {
		store = storage.NewCachedStore(durableStore, storage.CacheConfig{
			MaxSize: cfg.Cache.MaxSize,
			TTL:     cfg.Cache.TTL,
		})
		logger.Info("Read cache enabled",
			zap.Int("cache_size", cfg.Cache.MaxSize),
			zap.Duration("cache_ttl", cfg.Cache.TTL),
		)
	}

	if cfg.Raft.Enabled {
		logger.Info("Storage initialized with DurableStore (Raft cluster mode)",
			zap.String("data_dir", cfg.Storage.DataDir),
			zap.Bool("sync_on_write", cfg.Storage.SyncOnWrite),
			zap.Bool("cache_enabled", cfg.Cache.Enabled),
		)
	} else {
		logger.Info("Storage initialized with DurableStore (single-node mode)",
			zap.String("data_dir", cfg.Storage.DataDir),
			zap.Bool("sync_on_write", cfg.Storage.SyncOnWrite),
			zap.Bool("cache_enabled", cfg.Cache.Enabled),
		)
	}

	// Prepare TLS configuration if enabled (before Raft node creation)
	var tlsConfig *security.TLSConfig
	if cfg.TLS.Enabled {
		tlsConfig = &security.TLSConfig{
			CertFile:     cfg.TLS.CertFile,
			KeyFile:      cfg.TLS.KeyFile,
			ClientCAFile: cfg.TLS.CAFile,
			EnableMTLS:   cfg.TLS.EnableMTLS,
			MinVersion:   0x0303, // TLS 1.2
		}
		logger.Info("TLS configuration loaded",
			zap.String("cert", cfg.TLS.CertFile),
			zap.Bool("mtls", cfg.TLS.EnableMTLS),
		)
	}

	// Create Raft node if enabled
	var raftNode *consensus.RaftNode
	if cfg.Raft.Enabled {
		raftNode, err = consensus.NewRaftNode(consensus.RaftConfig{
			NodeID:    cfg.Raft.NodeID,
			RaftAddr:  cfg.Raft.RaftAddr,
			RaftDir:   cfg.Raft.RaftDir,
			Bootstrap: cfg.Raft.Bootstrap,
			JoinAddr:  cfg.Raft.JoinAddr,
			Store:     durableStore, // Pass concrete DurableStore type
			TLSConfig: tlsConfig,    // Pass TLS config for inter-node encryption
			Logger:    logger.Logger,
		})
		if err != nil {
			logger.Fatal("Failed to create Raft node", zap.Error(err))
		}
		defer raftNode.Shutdown()

		logger.Info("Raft node created",
			zap.String("node_id", cfg.Raft.NodeID),
			zap.String("raft_addr", cfg.Raft.RaftAddr),
			zap.Bool("bootstrap", cfg.Raft.Bootstrap),
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

	// Initialize authentication managers if enabled
	var userManager *auth.UserManager
	var apiKeyManager *auth.APIKeyManager
	var jwtManager *auth.JWTManager

	if cfg.Auth.Enabled {
		// Validate JWT secret
		if cfg.Auth.JWTSecret == "" {
			logger.Fatal("JWT secret is required when authentication is enabled (use --auth.jwt-secret or RAFTKV_AUTH__JWT_SECRET env var)")
		}

		// Create auth managers
		userManager = auth.NewUserManager(store)
		apiKeyManager = auth.NewAPIKeyManager(store)
		jwtManager = auth.NewJWTManager(cfg.Auth.JWTSecret, cfg.Auth.TokenExpiry)

		logger.Info("Authentication enabled",
			zap.Duration("token_expiry", cfg.Auth.TokenExpiry),
		)

		// Create default admin user if it doesn't exist
		users, err := userManager.ListUsers()
		if err != nil {
			logger.Fatal("Failed to list users", zap.Error(err))
		}

		if len(users) == 0 {
			// No users exist - create default admin
			adminUser, err := userManager.CreateUser("admin", "admin", auth.RoleAdmin)
			if err != nil {
				logger.Fatal("Failed to create default admin user", zap.Error(err))
			}
			logger.Info("Created default admin user",
				zap.String("username", "admin"),
				zap.String("password", "admin"),
				zap.String("warning", "Please change the default password!"),
			)

			// Generate admin API key if requested
			if cfg.Auth.AdminKey != "" {
				// For simplicity, we'll just log that a custom admin key was requested
				// In production, you'd want to properly handle this
				logger.Info("Admin API key bootstrap requested",
					zap.String("user_id", adminUser.ID),
				)
			}
		}
	}

	// Create servers
	if cfg.Raft.Enabled {
		// Use Raft-aware HTTP server
		raftHTTPServer := server.NewRaftHTTPServer(server.RaftHTTPServerConfig{
			Addr:            cfg.Server.HTTPAddr,
			RaftNode:        raftNode,
			Logger:          logger,
			Metrics:         metrics,
			ReadTimeout:     cfg.Performance.ReadTimeout,
			WriteTimeout:    cfg.Performance.WriteTimeout,
			MaxRequestSize:  cfg.Performance.MaxRequestSize,
			EnableRateLimit: cfg.Performance.EnableRateLimit,
			RateLimit:       cfg.Performance.RateLimit,
			TLSConfig:       tlsConfig,
		})

		// Start HTTP server in goroutine
		go func() {
			if err := raftHTTPServer.Start(); err != nil {
				logger.Fatal("Raft HTTP server failed", zap.Error(err))
			}
		}()

		logger.Info("Raft HTTP server started", zap.String("addr", cfg.Server.HTTPAddr))

		// Start gRPC server if enabled
		var grpcServer *server.GRPCServer
		if cfg.Server.EnableGRPC {
			grpcServer = server.NewGRPCServer(server.GRPCServerConfig{
				Addr:      cfg.Server.GRPCAddr,
				RaftNode:  raftNode,
				Logger:    logger.Logger,
				Metrics:   metrics,
				TLSConfig: tlsConfig,
			})

			go func() {
				if err := grpcServer.Start(); err != nil {
					logger.Fatal("gRPC server failed", zap.Error(err))
				}
			}()

			logger.Info("gRPC server started", zap.String("addr", cfg.Server.GRPCAddr))
		}

		logger.Info("RaftKV is ready to accept requests (cluster mode)",
			zap.Bool("grpc_enabled", cfg.Server.EnableGRPC),
		)

		// Wait for interrupt signal
		waitForShutdownRaft(logger, raftHTTPServer, raftNode, grpcServer)
	} else {
		// Use standard HTTP server (single-node mode)
		httpServer := server.NewHTTPServer(server.HTTPServerConfig{
			Addr:            cfg.Server.HTTPAddr,
			Store:           store,
			Logger:          logger,
			Metrics:         metrics,
			ReadTimeout:     cfg.Performance.ReadTimeout,
			WriteTimeout:    cfg.Performance.WriteTimeout,
			MaxRequestSize:  cfg.Performance.MaxRequestSize,
			EnableRateLimit: cfg.Performance.EnableRateLimit,
			RateLimit:       cfg.Performance.RateLimit,
			TLSConfig:       tlsConfig,
			AuthEnabled:     cfg.Auth.Enabled,
			UserManager:     userManager,
			APIKeyManager:   apiKeyManager,
			JWTManager:      jwtManager,
		})

		// Start server in goroutine
		go func() {
			if err := httpServer.Start(); err != nil {
				logger.Fatal("HTTP server failed", zap.Error(err))
			}
		}()

		logger.Info("HTTP server started", zap.String("addr", cfg.Server.HTTPAddr))
		logger.Info("RaftKV is ready to accept requests (single-node mode)")

		// Wait for interrupt signal
		waitForShutdown(logger, httpServer)
	}
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
