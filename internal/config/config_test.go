package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	flag "github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify defaults
	assert.Equal(t, ":8080", cfg.Server.HTTPAddr)
	assert.Equal(t, ":9090", cfg.Server.GRPCAddr)
	assert.False(t, cfg.Server.EnableGRPC)

	assert.Equal(t, "./data", cfg.Storage.DataDir)
	assert.True(t, cfg.Storage.SyncOnWrite)
	assert.Equal(t, 10000, cfg.Storage.SnapshotEvery)

	assert.True(t, cfg.WAL.BatchEnabled)
	assert.Equal(t, 100, cfg.WAL.BatchSize)
	assert.Equal(t, int64(1024*1024), cfg.WAL.BatchBytes)
	assert.Equal(t, 10*time.Millisecond, cfg.WAL.BatchWaitTime)
	assert.True(t, cfg.WAL.CompactionEnabled)
	assert.Equal(t, 100, cfg.WAL.CompactionMargin)

	assert.True(t, cfg.Cache.Enabled)
	assert.Equal(t, 10000, cfg.Cache.MaxSize)
	assert.Equal(t, time.Duration(0), cfg.Cache.TTL)

	assert.False(t, cfg.Raft.Enabled)
	assert.Equal(t, "node1", cfg.Raft.NodeID)
	assert.Equal(t, "localhost:7000", cfg.Raft.RaftAddr)

	assert.False(t, cfg.Auth.Enabled)
	assert.Equal(t, 1*time.Hour, cfg.Auth.TokenExpiry)

	assert.False(t, cfg.TLS.Enabled)
	assert.False(t, cfg.TLS.EnableMTLS)

	assert.Equal(t, "info", cfg.Observability.LogLevel)
	assert.True(t, cfg.Observability.MetricsEnabled)

	assert.Equal(t, 10*time.Second, cfg.Performance.ReadTimeout)
	assert.Equal(t, 10*time.Second, cfg.Performance.WriteTimeout)
	assert.Equal(t, int64(1024*1024), cfg.Performance.MaxRequestSize)
	assert.False(t, cfg.Performance.EnableRateLimit)
	assert.Equal(t, 1000, cfg.Performance.RateLimit)
}

func TestLoadFromYAML(t *testing.T) {
	yamlConfig := `
server:
  http_addr: ":8888"
  grpc_addr: ":9999"
  enable_grpc: true

storage:
  data_dir: "/var/lib/raftkv"
  sync_on_write: false
  snapshot_every: 5000

wal:
  batch_enabled: true
  batch_size: 200
  batch_bytes: 2097152
  batch_wait_time: 20ms
  compaction_enabled: true
  compaction_margin: 200

cache:
  enabled: true
  max_size: 20000
  ttl: 5m

raft:
  enabled: true
  node_id: "node2"
  raft_addr: "192.168.1.10:7000"
  raft_dir: "/var/lib/raftkv/raft"
  bootstrap: true
  join_addr: ""

auth:
  enabled: true
  jwt_secret: "my-secret-key"
  token_expiry: 2h
  admin_key: "admin-key-123"

tls:
  enabled: true
  cert_file: "/etc/raftkv/server.crt"
  key_file: "/etc/raftkv/server.key"
  ca_file: "/etc/raftkv/ca.crt"
  enable_mtls: true

observability:
  log_level: "debug"
  metrics_enabled: true

performance:
  read_timeout: 30s
  write_timeout: 30s
  max_request_size: 5242880
  enable_rate_limit: true
  rate_limit: 2000
`

	// Create temp file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := Load(LoadOptions{ConfigFile: configFile})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify values
	assert.Equal(t, ":8888", cfg.Server.HTTPAddr)
	assert.Equal(t, ":9999", cfg.Server.GRPCAddr)
	assert.True(t, cfg.Server.EnableGRPC)

	assert.Equal(t, "/var/lib/raftkv", cfg.Storage.DataDir)
	assert.False(t, cfg.Storage.SyncOnWrite)
	assert.Equal(t, 5000, cfg.Storage.SnapshotEvery)

	assert.True(t, cfg.WAL.BatchEnabled)
	assert.Equal(t, 200, cfg.WAL.BatchSize)
	assert.Equal(t, int64(2097152), cfg.WAL.BatchBytes)
	assert.Equal(t, 20*time.Millisecond, cfg.WAL.BatchWaitTime)

	assert.True(t, cfg.Cache.Enabled)
	assert.Equal(t, 20000, cfg.Cache.MaxSize)
	assert.Equal(t, 5*time.Minute, cfg.Cache.TTL)

	assert.True(t, cfg.Raft.Enabled)
	assert.Equal(t, "node2", cfg.Raft.NodeID)
	assert.Equal(t, "192.168.1.10:7000", cfg.Raft.RaftAddr)
	assert.Equal(t, "/var/lib/raftkv/raft", cfg.Raft.RaftDir)
	assert.True(t, cfg.Raft.Bootstrap)

	assert.True(t, cfg.Auth.Enabled)
	assert.Equal(t, "my-secret-key", cfg.Auth.JWTSecret)
	assert.Equal(t, 2*time.Hour, cfg.Auth.TokenExpiry)
	assert.Equal(t, "admin-key-123", cfg.Auth.AdminKey)

	assert.True(t, cfg.TLS.Enabled)
	assert.Equal(t, "/etc/raftkv/server.crt", cfg.TLS.CertFile)
	assert.Equal(t, "/etc/raftkv/server.key", cfg.TLS.KeyFile)
	assert.Equal(t, "/etc/raftkv/ca.crt", cfg.TLS.CAFile)
	assert.True(t, cfg.TLS.EnableMTLS)

	assert.Equal(t, "debug", cfg.Observability.LogLevel)
	assert.True(t, cfg.Observability.MetricsEnabled)

	assert.Equal(t, 30*time.Second, cfg.Performance.ReadTimeout)
	assert.Equal(t, 30*time.Second, cfg.Performance.WriteTimeout)
	assert.Equal(t, int64(5242880), cfg.Performance.MaxRequestSize)
	assert.True(t, cfg.Performance.EnableRateLimit)
	assert.Equal(t, 2000, cfg.Performance.RateLimit)
}

func TestLoadFromJSON(t *testing.T) {
	jsonConfig := `{
  "server": {
    "http_addr": ":8888",
    "grpc_addr": ":9999",
    "enable_grpc": true
  },
  "storage": {
    "data_dir": "/var/lib/raftkv",
    "sync_on_write": false,
    "snapshot_every": 5000
  },
  "auth": {
    "enabled": true,
    "jwt_secret": "test-secret",
    "token_expiry": "2h",
    "admin_key": ""
  },
  "observability": {
    "log_level": "debug",
    "metrics_enabled": true
  }
}`

	// Create temp file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configFile, []byte(jsonConfig), 0644)
	require.NoError(t, err)

	// Load config
	cfg, err := Load(LoadOptions{ConfigFile: configFile})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify values
	assert.Equal(t, ":8888", cfg.Server.HTTPAddr)
	assert.Equal(t, ":9999", cfg.Server.GRPCAddr)
	assert.True(t, cfg.Server.EnableGRPC)
	assert.Equal(t, "/var/lib/raftkv", cfg.Storage.DataDir)
	assert.False(t, cfg.Storage.SyncOnWrite)
	assert.Equal(t, 5000, cfg.Storage.SnapshotEvery)
	assert.True(t, cfg.Auth.Enabled)
	assert.Equal(t, "test-secret", cfg.Auth.JWTSecret)
	assert.Equal(t, "debug", cfg.Observability.LogLevel)
}

func TestLoadFromEnvVars(t *testing.T) {
	// Set test environment variables (use double underscore for nested keys)
	os.Setenv("RAFTKV_AUTH__JWT_SECRET", "secret-from-env")
	os.Setenv("RAFTKV_STORAGE__DATA_DIR", "/tmp/test-data")
	os.Setenv("RAFTKV_SERVER__HTTP_ADDR", ":9000")
	defer func() {
		os.Unsetenv("RAFTKV_AUTH__JWT_SECRET")
		os.Unsetenv("RAFTKV_STORAGE__DATA_DIR")
		os.Unsetenv("RAFTKV_SERVER__HTTP_ADDR")
	}()

	// Load config
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify environment variables were loaded
	assert.Equal(t, "secret-from-env", cfg.Auth.JWTSecret)
	assert.Equal(t, "/tmp/test-data", cfg.Storage.DataDir)
	assert.Equal(t, ":9000", cfg.Server.HTTPAddr)
}

func TestLoadWithFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	RegisterFlags(fs)

	// Parse some flags (using dotted notation)
	err := fs.Parse([]string{
		"--server.http-addr", ":7777",
		"--storage.data-dir", "/custom/data",
		"--raft.enabled",
		"--raft.node-id", "custom-node",
	})
	require.NoError(t, err)

	// Load config
	cfg, err := Load(LoadOptions{Flags: fs})
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify flags were applied
	assert.Equal(t, ":7777", cfg.Server.HTTPAddr)
	assert.Equal(t, "/custom/data", cfg.Storage.DataDir)
	assert.True(t, cfg.Raft.Enabled)
	assert.Equal(t, "custom-node", cfg.Raft.NodeID)
}

func TestLoadPrecedence(t *testing.T) {
	// Create config file
	yamlConfig := `
server:
  http_addr: ":8888"
storage:
  data_dir: "/config/file/data"
auth:
  jwt_secret: "from-config-file"
`
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(yamlConfig), 0644)
	require.NoError(t, err)

	// Set environment variable (use double underscore)
	os.Setenv("RAFTKV_STORAGE__DATA_DIR", "/from/env")
	defer os.Unsetenv("RAFTKV_STORAGE__DATA_DIR")

	// Create flags
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	RegisterFlags(fs)
	err = fs.Parse([]string{"--server.http-addr", ":9999"})
	require.NoError(t, err)

	// Load config with all sources
	cfg, err := Load(LoadOptions{
		ConfigFile: configFile,
		Flags:      fs,
	})
	require.NoError(t, err)

	// Verify precedence: flags > env > config file > defaults
	assert.Equal(t, ":9999", cfg.Server.HTTPAddr)               // From flags (highest)
	assert.Equal(t, "/from/env", cfg.Storage.DataDir)           // From env (overrides config file)
	assert.Equal(t, "from-config-file", cfg.Auth.JWTSecret)     // From config file
	assert.Equal(t, ":9090", cfg.Server.GRPCAddr)               // From defaults (lowest)
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*Config)
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid config",
			modify:    func(c *Config) {},
			wantError: false,
		},
		{
			name: "auth enabled without jwt secret",
			modify: func(c *Config) {
				c.Auth.Enabled = true
				c.Auth.JWTSecret = ""
			},
			wantError: true,
			errorMsg:  "auth.jwt_secret is required",
		},
		{
			name: "tls enabled without cert",
			modify: func(c *Config) {
				c.TLS.Enabled = true
				c.TLS.CertFile = ""
			},
			wantError: true,
			errorMsg:  "tls.cert_file is required",
		},
		{
			name: "invalid log level",
			modify: func(c *Config) {
				c.Observability.LogLevel = "invalid"
			},
			wantError: true,
			errorMsg:  "observability.log_level must be one of",
		},
		{
			name: "raft enabled without node id",
			modify: func(c *Config) {
				c.Raft.Enabled = true
				c.Raft.NodeID = ""
			},
			wantError: true,
			errorMsg:  "raft.node_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(LoadOptions{})
			require.NoError(t, err)

			tt.modify(cfg)

			err = cfg.Validate()
			if tt.wantError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadFileNotFound(t *testing.T) {
	_, err := Load(LoadOptions{ConfigFile: "/nonexistent/config.yaml"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load config file")
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte("invalid: yaml: content: ["), 0644)
	require.NoError(t, err)

	_, err = Load(LoadOptions{ConfigFile: configFile})
	assert.Error(t, err)
}

func TestLoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configFile, []byte("{invalid json}"), 0644)
	require.NoError(t, err)

	_, err = Load(LoadOptions{ConfigFile: configFile})
	assert.Error(t, err)
}
