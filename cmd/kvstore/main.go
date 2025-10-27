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

	"github.com/RashikAnsar/raftkv/internal/observability"
	"github.com/RashikAnsar/raftkv/internal/server"
	"github.com/RashikAnsar/raftkv/internal/storage"
)

type Config struct {
	// Server config
	HTTPAddr string

	// Storage config
	DataDir       string
	SyncOnWrite   bool
	SnapshotEvery int

	// Observability
	LogLevel string

	// Performance
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	MaxRequestSize  int64
	EnableRateLimit bool
	RateLimit       int
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
		zap.String("data_dir", config.DataDir),
	)

	// Create storage
	store, err := storage.NewDurableStore(storage.DurableStoreConfig{
		DataDir:       config.DataDir,
		SyncOnWrite:   config.SyncOnWrite,
		SnapshotEvery: config.SnapshotEvery,
	})
	if err != nil {
		logger.Fatal("Failed to create store", zap.Error(err))
	}
	defer store.Close()

	logger.Info("Storage initialized",
		zap.String("data_dir", config.DataDir),
		zap.Bool("sync_on_write", config.SyncOnWrite),
		zap.Int("snapshot_every", config.SnapshotEvery),
	)

	// Create metrics
	metrics := observability.NewMetrics()

	// Create HTTP server
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
	logger.Info("RaftKV is ready to accept requests")

	// Wait for interrupt signal
	waitForShutdown(logger, httpServer, store)
}

func parseFlags() Config {
	config := Config{}

	// Server flags
	flag.StringVar(&config.HTTPAddr, "http-addr", ":8080", "HTTP server address")

	// Storage flags
	flag.StringVar(&config.DataDir, "data-dir", "./data", "Data directory")
	flag.BoolVar(&config.SyncOnWrite, "sync-on-write", true, "Sync writes to disk (durable but slower)")
	flag.IntVar(&config.SnapshotEvery, "snapshot-every", 10000, "Snapshot every N operations")

	// Observability flags
	flag.StringVar(&config.LogLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	// Performance flags
	readTimeoutSec := flag.Int("read-timeout", 10, "Read timeout in seconds")
	writeTimeoutSec := flag.Int("write-timeout", 10, "Write timeout in seconds")
	flag.Int64Var(&config.MaxRequestSize, "max-request-size", 1024*1024, "Max request body size in bytes")
	flag.BoolVar(&config.EnableRateLimit, "enable-rate-limit", false, "Enable rate limiting")
	flag.IntVar(&config.RateLimit, "rate-limit", 1000, "Requests per second limit")

	flag.Parse()

	config.ReadTimeout = time.Duration(*readTimeoutSec) * time.Second
	config.WriteTimeout = time.Duration(*writeTimeoutSec) * time.Second

	return config
}

func waitForShutdown(logger *observability.Logger, httpServer *server.HTTPServer, store storage.Store) {
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

	// Close store
	logger.Info("Closing store...")
	if err := store.Close(); err != nil {
		logger.Error("Store close error", zap.Error(err))
	}

	logger.Info("Shutdown complete")
}
