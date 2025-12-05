package backup

import (
	"encoding/json"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the complete backup configuration
type Config struct {
	// Provider configuration
	Provider ProviderConfig `json:"provider" yaml:"provider"`

	// Backup settings
	Backup BackupSettings `json:"backup" yaml:"backup"`

	// Schedule settings (optional)
	Schedule *ScheduleSettings `json:"schedule,omitempty" yaml:"schedule,omitempty"`

	// Retention settings
	Retention RetentionSettings `json:"retention" yaml:"retention"`
}

// BackupSettings contains backup-specific configuration
type BackupSettings struct {
	Compression CompressionSettings `json:"compression" yaml:"compression"`
	Encryption  EncryptionSettings  `json:"encryption" yaml:"encryption"`
	NodeID      string              `json:"node_id" yaml:"node_id"`
	ClusterID   string              `json:"cluster_id" yaml:"cluster_id"`
	Version     string              `json:"version" yaml:"version"`
	Prefix      string              `json:"prefix" yaml:"prefix"` // Storage prefix for backups
}

// CompressionSettings contains compression configuration
type CompressionSettings struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Type    string `json:"type" yaml:"type"`       // gzip, zstd
	Level   int    `json:"level" yaml:"level"`     // 1-9 for gzip
}

// EncryptionSettings contains encryption configuration
type EncryptionSettings struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Type    string `json:"type" yaml:"type"`         // aes-256-gcm
	KeyFile string `json:"key_file" yaml:"key_file"` // Path to encryption key file
}

// ScheduleSettings contains scheduling configuration
type ScheduleSettings struct {
	Enabled        bool   `json:"enabled" yaml:"enabled"`
	CronExpression string `json:"cron_expression" yaml:"cron_expression"` // e.g., "0 0 * * *"
	Timezone       string `json:"timezone" yaml:"timezone"`               // e.g., "America/New_York"
	EnablePruning  bool   `json:"enable_pruning" yaml:"enable_pruning"`
}

// RetentionSettings contains retention policy configuration
type RetentionSettings struct {
	RetentionDays int  `json:"retention_days" yaml:"retention_days"` // Keep backups for N days
	MaxBackups    int  `json:"max_backups" yaml:"max_backups"`       // Keep at most N backups
	MinBackups    int  `json:"min_backups" yaml:"min_backups"`       // Always keep at least N backups
	PruneOnBackup bool `json:"prune_on_backup" yaml:"prune_on_backup"`
}

// LoadConfig loads backup configuration from a file
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config

	// Detect format by extension
	ext := path[len(path)-5:]
	if ext == ".yaml" || ext == ".yml" {
		if err := yaml.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse YAML config: %w", err)
		}
	} else if path[len(path)-5:] == ".json" {
		if err := json.Unmarshal(data, &config); err != nil {
			return nil, fmt.Errorf("failed to parse JSON config: %w", err)
		}
	} else {
		return nil, fmt.Errorf("unsupported config format: %s (use .yaml or .json)", path)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate provider
	if c.Provider.Type == "" {
		return fmt.Errorf("provider type is required")
	}

	switch c.Provider.Type {
	case ProviderTypeLocal:
		if c.Provider.LocalPath == "" {
			return fmt.Errorf("local path is required for local provider")
		}
	case ProviderTypeS3, ProviderTypeMinIO:
		if c.Provider.S3Bucket == "" {
			return fmt.Errorf("S3 bucket is required")
		}
		if c.Provider.S3Region == "" {
			return fmt.Errorf("S3 region is required")
		}
	case ProviderTypeGCS:
		if c.Provider.GCSBucket == "" {
			return fmt.Errorf("GCS bucket is required")
		}
	case ProviderTypeAzure:
		if c.Provider.AzureContainer == "" {
			return fmt.Errorf("Azure container is required")
		}
	default:
		return fmt.Errorf("unsupported provider type: %s", c.Provider.Type)
	}

	// Validate compression
	if c.Backup.Compression.Enabled {
		if c.Backup.Compression.Type != "gzip" && c.Backup.Compression.Type != "none" {
			return fmt.Errorf("unsupported compression type: %s", c.Backup.Compression.Type)
		}
	}

	// Validate encryption
	if c.Backup.Encryption.Enabled {
		if c.Backup.Encryption.Type != "aes-256-gcm" {
			return fmt.Errorf("unsupported encryption type: %s", c.Backup.Encryption.Type)
		}
		if c.Backup.Encryption.KeyFile == "" {
			return fmt.Errorf("encryption key file is required when encryption is enabled")
		}
	}

	// Validate schedule
	if c.Schedule != nil && c.Schedule.Enabled {
		if c.Schedule.CronExpression == "" {
			return fmt.Errorf("cron expression is required when scheduling is enabled")
		}
	}

	return nil
}

// ToBackupConfig converts Config to BackupConfig
func (c *Config) ToBackupConfig() (BackupConfig, error) {
	cfg := BackupConfig{
		NodeID:         c.Backup.NodeID,
		ClusterID:      c.Backup.ClusterID,
		Version:        c.Backup.Version,
		RetentionDays:  c.Retention.RetentionDays,
		MaxBackups:     c.Retention.MaxBackups,
		BackupPrefix:   c.Backup.Prefix,
	}

	// Set compression
	if c.Backup.Compression.Enabled {
		cfg.CompressionType = CompressionType(c.Backup.Compression.Type)
		cfg.CompressionLevel = c.Backup.Compression.Level
	} else {
		cfg.CompressionType = CompressionTypeNone
	}

	// Set encryption
	if c.Backup.Encryption.Enabled {
		cfg.EncryptionType = EncryptionType(c.Backup.Encryption.Type)
		// Load encryption key
		key, err := os.ReadFile(c.Backup.Encryption.KeyFile)
		if err != nil {
			return cfg, fmt.Errorf("failed to read encryption key: %w", err)
		}
		if len(key) != 32 {
			return cfg, fmt.Errorf("encryption key must be 32 bytes, got %d", len(key))
		}
		cfg.EncryptionKey = key
	} else {
		cfg.EncryptionType = EncryptionTypeNone
	}

	return cfg, nil
}

// SaveConfig saves configuration to a file
func (c *Config) SaveConfig(path string) error {
	var data []byte
	var err error

	// Detect format by extension
	if path[len(path)-5:] == ".yaml" || path[len(path)-4:] == ".yml" {
		data, err = yaml.Marshal(c)
	} else {
		data, err = json.MarshalIndent(c, "", "  ")
	}

	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}
