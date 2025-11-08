package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/json"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	flag "github.com/spf13/pflag"
)

// Config represents the complete configuration for RaftKV
type Config struct {
	Server        ServerConfig        `koanf:"server"`
	Storage       StorageConfig       `koanf:"storage"`
	WAL           WALConfig           `koanf:"wal"`
	Cache         CacheConfig         `koanf:"cache"`
	Raft          RaftConfig          `koanf:"raft"`
	Auth          AuthConfig          `koanf:"auth"`
	TLS           TLSConfig           `koanf:"tls"`
	Observability ObservabilityConfig `koanf:"observability"`
	Performance   PerformanceConfig   `koanf:"performance"`
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	HTTPAddr   string `koanf:"http_addr"`
	GRPCAddr   string `koanf:"grpc_addr"`
	EnableGRPC bool   `koanf:"enable_grpc"`
}

// StorageConfig holds storage-related configuration
type StorageConfig struct {
	DataDir       string `koanf:"data_dir"`
	SyncOnWrite   bool   `koanf:"sync_on_write"`
	SnapshotEvery int    `koanf:"snapshot_every"`
}

// WALConfig holds write-ahead log configuration
type WALConfig struct {
	BatchEnabled      bool          `koanf:"batch_enabled"`
	BatchSize         int           `koanf:"batch_size"`
	BatchBytes        int64         `koanf:"batch_bytes"`
	BatchWaitTime     time.Duration `koanf:"batch_wait_time"`
	CompactionEnabled bool          `koanf:"compaction_enabled"`
	CompactionMargin  int           `koanf:"compaction_margin"`
}

// CacheConfig holds cache-related configuration
type CacheConfig struct {
	Enabled bool          `koanf:"enabled"`
	MaxSize int           `koanf:"max_size"`
	TTL     time.Duration `koanf:"ttl"`
}

// RaftConfig holds Raft consensus configuration
type RaftConfig struct {
	Enabled   bool   `koanf:"enabled"`
	NodeID    string `koanf:"node_id"`
	RaftAddr  string `koanf:"raft_addr"`
	RaftDir   string `koanf:"raft_dir"`
	Bootstrap bool   `koanf:"bootstrap"`
	JoinAddr  string `koanf:"join_addr"`
}

// AuthConfig holds authentication and authorization configuration
type AuthConfig struct {
	Enabled     bool          `koanf:"enabled"`
	JWTSecret   string        `koanf:"jwt_secret"`
	TokenExpiry time.Duration `koanf:"token_expiry"`
	AdminKey    string        `koanf:"admin_key"`
}

// TLSConfig holds TLS/mTLS configuration
type TLSConfig struct {
	Enabled    bool   `koanf:"enabled"`
	CertFile   string `koanf:"cert_file"`
	KeyFile    string `koanf:"key_file"`
	CAFile     string `koanf:"ca_file"`
	EnableMTLS bool   `koanf:"enable_mtls"`
}

// ObservabilityConfig holds logging and metrics configuration
type ObservabilityConfig struct {
	LogLevel       string `koanf:"log_level"`
	MetricsEnabled bool   `koanf:"metrics_enabled"`
}

// PerformanceConfig holds performance tuning configuration
type PerformanceConfig struct {
	ReadTimeout     time.Duration `koanf:"read_timeout"`
	WriteTimeout    time.Duration `koanf:"write_timeout"`
	MaxRequestSize  int64         `koanf:"max_request_size"`
	EnableRateLimit bool          `koanf:"enable_rate_limit"`
	RateLimit       int           `koanf:"rate_limit"`
}

// LoadOptions holds options for loading configuration
type LoadOptions struct {
	ConfigFile string
	Flags      *flag.FlagSet
}

// Load loads configuration from multiple sources with the following precedence (highest to lowest):
// 1. Command-line flags
// 2. Environment variables (prefix: RAFTKV_)
// 3. Config file (YAML or JSON)
// 4. Defaults
func Load(opts LoadOptions) (*Config, error) {
	k := koanf.New(".")

	// Load defaults first
	if err := loadDefaults(k); err != nil {
		return nil, fmt.Errorf("failed to load defaults: %w", err)
	}

	// Load from config file if provided
	if opts.ConfigFile != "" {
		if strings.HasSuffix(opts.ConfigFile, ".json") {
			if err := k.Load(file.Provider(opts.ConfigFile), json.Parser()); err != nil {
				return nil, fmt.Errorf("failed to load config file: %w", err)
			}
		} else {
			if err := k.Load(file.Provider(opts.ConfigFile), yaml.Parser()); err != nil {
				return nil, fmt.Errorf("failed to load config file: %w", err)
			}
		}
	}

	// Load from environment variables (RAFTKV_SERVER__HTTP_ADDR -> server.http_addr)
	// Note: Use double underscore (__) as delimiter for nested keys
	if err := k.Load(env.ProviderWithValue("RAFTKV_", ".", func(s string, v string) (string, interface{}) {
		// Convert RAFTKV_SERVER__HTTP_ADDR to server.http_addr
		key := strings.Replace(strings.ToLower(
			strings.TrimPrefix(s, "RAFTKV_")), "__", ".", -1)
		return key, v
	}), nil); err != nil {
		return nil, fmt.Errorf("failed to load environment variables: %w", err)
	}

	// Load from command-line flags if provided
	if opts.Flags != nil {
		// Use posflag with proper delimiter mapping
		if err := k.Load(posflag.ProviderWithFlag(opts.Flags, ".", k, func(f *flag.Flag) (string, interface{}) {
			// Convert flag name hyphens to underscores for koanf
			key := strings.ReplaceAll(f.Name, "-", "_")
			return key, posflag.FlagVal(opts.Flags, f)
		}), nil); err != nil {
			return nil, fmt.Errorf("failed to load flags: %w", err)
		}
	}

	// Unmarshal into Config struct
	var config Config
	if err := k.Unmarshal("", &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// loadDefaults loads default values into koanf
func loadDefaults(k *koanf.Koanf) error {
	defaults := map[string]interface{}{
		"server.http_addr":   ":8080",
		"server.grpc_addr":   ":9090",
		"server.enable_grpc": false,

		"storage.data_dir":       "./data",
		"storage.sync_on_write":  true,
		"storage.snapshot_every": 10000,

		"wal.batch_enabled":      true,
		"wal.batch_size":         100,
		"wal.batch_bytes":        1024 * 1024,
		"wal.batch_wait_time":    10 * time.Millisecond,
		"wal.compaction_enabled": true,
		"wal.compaction_margin":  100,

		"cache.enabled":  true,
		"cache.max_size": 10000,
		"cache.ttl":      0,

		"raft.enabled":   false,
		"raft.node_id":   "node1",
		"raft.raft_addr": "localhost:7000",
		"raft.raft_dir":  "./data/raft",
		"raft.bootstrap": false,
		"raft.join_addr": "",

		"auth.enabled":      false,
		"auth.jwt_secret":   "",
		"auth.token_expiry": 1 * time.Hour,
		"auth.admin_key":    "",

		"tls.enabled":     false,
		"tls.cert_file":   "certs/server-cert.pem",
		"tls.key_file":    "certs/server-key.pem",
		"tls.ca_file":     "certs/ca-cert.pem",
		"tls.enable_mtls": false,

		"observability.log_level":       "info",
		"observability.metrics_enabled": true,

		"performance.read_timeout":      10 * time.Second,
		"performance.write_timeout":     10 * time.Second,
		"performance.max_request_size":  1024 * 1024,
		"performance.enable_rate_limit": false,
		"performance.rate_limit":        1000,
	}

	return k.Load(confmap.Provider(defaults, "."), nil)
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate server config
	if c.Server.HTTPAddr == "" {
		return fmt.Errorf("server.http_addr is required")
	}
	if c.Server.EnableGRPC && c.Server.GRPCAddr == "" {
		return fmt.Errorf("server.grpc_addr is required when gRPC is enabled")
	}

	// Validate storage config
	if c.Storage.DataDir == "" {
		return fmt.Errorf("storage.data_dir is required")
	}
	if c.Storage.SnapshotEvery <= 0 {
		return fmt.Errorf("storage.snapshot_every must be positive")
	}

	// Validate WAL config
	if c.WAL.BatchEnabled {
		if c.WAL.BatchSize <= 0 {
			return fmt.Errorf("wal.batch_size must be positive when batching is enabled")
		}
		if c.WAL.BatchBytes <= 0 {
			return fmt.Errorf("wal.batch_bytes must be positive when batching is enabled")
		}
		if c.WAL.BatchWaitTime <= 0 {
			return fmt.Errorf("wal.batch_wait_time must be positive when batching is enabled")
		}
	}
	if c.WAL.CompactionEnabled && c.WAL.CompactionMargin < 0 {
		return fmt.Errorf("wal.compaction_margin must be non-negative")
	}

	// Validate cache config
	if c.Cache.Enabled && c.Cache.MaxSize <= 0 {
		return fmt.Errorf("cache.max_size must be positive when cache is enabled")
	}

	// Validate Raft config
	if c.Raft.Enabled {
		if c.Raft.NodeID == "" {
			return fmt.Errorf("raft.node_id is required when Raft is enabled")
		}
		if c.Raft.RaftAddr == "" {
			return fmt.Errorf("raft.raft_addr is required when Raft is enabled")
		}
		if c.Raft.RaftDir == "" {
			return fmt.Errorf("raft.raft_dir is required when Raft is enabled")
		}
	}

	// Validate auth config
	if c.Auth.Enabled {
		if c.Auth.JWTSecret == "" {
			return fmt.Errorf("auth.jwt_secret is required when authentication is enabled")
		}
		if c.Auth.TokenExpiry <= 0 {
			return fmt.Errorf("auth.token_expiry must be positive")
		}
	}

	// Validate TLS config
	if c.TLS.Enabled {
		if c.TLS.CertFile == "" {
			return fmt.Errorf("tls.cert_file is required when TLS is enabled")
		}
		if c.TLS.KeyFile == "" {
			return fmt.Errorf("tls.key_file is required when TLS is enabled")
		}
		if c.TLS.EnableMTLS && c.TLS.CAFile == "" {
			return fmt.Errorf("tls.ca_file is required when mTLS is enabled")
		}
	}

	// Validate observability config
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.Observability.LogLevel] {
		return fmt.Errorf("observability.log_level must be one of: debug, info, warn, error")
	}

	// Validate performance config
	if c.Performance.ReadTimeout <= 0 {
		return fmt.Errorf("performance.read_timeout must be positive")
	}
	if c.Performance.WriteTimeout <= 0 {
		return fmt.Errorf("performance.write_timeout must be positive")
	}
	if c.Performance.MaxRequestSize <= 0 {
		return fmt.Errorf("performance.max_request_size must be positive")
	}
	if c.Performance.EnableRateLimit && c.Performance.RateLimit <= 0 {
		return fmt.Errorf("performance.rate_limit must be positive when rate limiting is enabled")
	}

	return nil
}

// RegisterFlags registers command-line flags for all configuration options
// Flags use dotted notation to map to nested config fields
func RegisterFlags(fs *flag.FlagSet) {
	// Server flags
	fs.String("server.http-addr", ":8080", "HTTP server address")
	fs.String("server.grpc-addr", ":9090", "gRPC server address")
	fs.Bool("server.enable-grpc", false, "Enable gRPC server")

	// Storage flags
	fs.String("storage.data-dir", "./data", "Data directory")
	fs.Bool("storage.sync-on-write", true, "Sync writes to disk (durable but slower)")
	fs.Int("storage.snapshot-every", 10000, "Snapshot every N operations")

	// WAL flags
	fs.Bool("wal.batch-enabled", true, "Enable WAL batch writes")
	fs.Int("wal.batch-size", 100, "Maximum number of operations per batch")
	fs.Int64("wal.batch-bytes", 1024*1024, "Maximum bytes per batch")
	fs.Duration("wal.batch-wait-time", 10*time.Millisecond, "Maximum wait time for batch")
	fs.Bool("wal.compaction-enabled", true, "Enable WAL compaction")
	fs.Int("wal.compaction-margin", 100, "Safety margin for WAL compaction")

	// Cache flags
	fs.Bool("cache.enabled", true, "Enable read cache")
	fs.Int("cache.max-size", 10000, "Max number of entries in cache")
	fs.Duration("cache.ttl", 0, "Cache TTL (0 = no expiration)")

	// Raft flags
	fs.Bool("raft.enabled", false, "Enable Raft consensus")
	fs.String("raft.node-id", "node1", "Unique node ID")
	fs.String("raft.raft-addr", "localhost:7000", "Raft bind address")
	fs.String("raft.raft-dir", "./data/raft", "Raft data directory")
	fs.Bool("raft.bootstrap", false, "Bootstrap new cluster")
	fs.String("raft.join-addr", "", "Join existing cluster at this address")

	// Auth flags
	fs.Bool("auth.enabled", false, "Enable authentication and authorization")
	fs.String("auth.jwt-secret", "", "JWT signing secret (required if auth enabled)")
	fs.Duration("auth.token-expiry", 1*time.Hour, "JWT token expiry")
	fs.String("auth.admin-key", "", "Initial admin API key for bootstrap (optional)")

	// TLS flags
	fs.Bool("tls.enabled", false, "Enable TLS/HTTPS")
	fs.String("tls.cert-file", "certs/server-cert.pem", "TLS certificate file")
	fs.String("tls.key-file", "certs/server-key.pem", "TLS key file")
	fs.String("tls.ca-file", "certs/ca-cert.pem", "TLS CA certificate (for mTLS)")
	fs.Bool("tls.enable-mtls", false, "Enable mutual TLS (client authentication)")

	// Observability flags
	fs.String("observability.log-level", "info", "Log level (debug, info, warn, error)")
	fs.Bool("observability.metrics-enabled", true, "Enable Prometheus metrics")

	// Performance flags
	fs.Duration("performance.read-timeout", 10*time.Second, "Read timeout")
	fs.Duration("performance.write-timeout", 10*time.Second, "Write timeout")
	fs.Int64("performance.max-request-size", 1024*1024, "Max request body size in bytes")
	fs.Bool("performance.enable-rate-limit", false, "Enable rate limiting")
	fs.Int("performance.rate-limit", 1000, "Requests per second limit")
}
