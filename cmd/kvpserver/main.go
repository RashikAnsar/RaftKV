package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RashikAnsar/raftkv/internal/kvp"
	"github.com/RashikAnsar/raftkv/internal/storage"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// Command-line flags
	var (
		addr            = flag.String("addr", ":6379", "TCP address to listen on (e.g., :6379)")
		readTimeout     = flag.Duration("read-timeout", 30*time.Second, "Read timeout for connections")
		writeTimeout    = flag.Duration("write-timeout", 30*time.Second, "Write timeout for connections")
		maxConnections  = flag.Int("max-connections", 10000, "Maximum concurrent connections")
		idleTimeout     = flag.Duration("idle-timeout", 5*time.Minute, "Idle connection timeout")
		shutdownTimeout = flag.Duration("shutdown-timeout", 30*time.Second, "Graceful shutdown timeout")
		ttlScanInterval = flag.Duration("ttl-scan-interval", 1*time.Minute, "TTL scan interval for expired keys")
		logLevel        = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	)

	flag.Parse()

	// Initialize logger
	logger, err := createLogger(*logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	logger.Info("Starting KVP server",
		zap.String("addr", *addr),
		zap.Duration("read_timeout", *readTimeout),
		zap.Duration("write_timeout", *writeTimeout),
		zap.Int("max_connections", *maxConnections),
	)

	// Create storage
	store := storage.NewMemoryStore()
	defer store.Close()

	// Start TTL manager
	ttlConfig := storage.TTLManagerConfig{
		ScanInterval: *ttlScanInterval,
		BatchSize:    100,
	}
	ttlManager := storage.NewTTLManager(store, logger, ttlConfig)
	ttlManager.Start()
	defer ttlManager.Stop()

	// Create KVP server
	config := kvp.ServerConfig{
		Addr:            *addr,
		ReadTimeout:     *readTimeout,
		WriteTimeout:    *writeTimeout,
		MaxConnections:  *maxConnections,
		IdleTimeout:     *idleTimeout,
		ShutdownTimeout: *shutdownTimeout,
	}

	server := kvp.NewServer(config, store, logger)

	// Start server
	if err := server.Start(); err != nil {
		logger.Fatal("Failed to start server", zap.Error(err))
	}

	logger.Info("KVP server started successfully",
		zap.String("addr", server.Addr()),
		zap.String("protocol", "Redis-compatible (RESP)"),
	)

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutdown signal received, stopping server...")

	// Stop server
	if err := server.Stop(); err != nil {
		logger.Error("Error stopping server", zap.Error(err))
	}

	logger.Info("KVP server stopped")
}

// createLogger creates a zap logger with the specified level
func createLogger(level string) (*zap.Logger, error) {
	var zapLevel zap.AtomicLevel

	switch level {
	case "debug":
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	config := zap.NewProductionConfig()
	config.Level = zapLevel
	config.Encoding = "console"
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	return config.Build()
}
