package replication

import (
	"fmt"
	"time"
)

// Config defines the replication configuration
type Config struct {
	// Enabled determines if replication is active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// DatacenterID is the unique identifier for this datacenter
	DatacenterID string `json:"datacenter_id" yaml:"datacenter_id"`

	// Role defines the replication role (primary or replica)
	Role ReplicationRole `json:"role" yaml:"role"`

	// Targets are the remote datacenters to replicate to
	Targets []DatacenterTarget `json:"targets" yaml:"targets"`

	// BufferSize is the CDC subscriber buffer size
	BufferSize int `json:"buffer_size" yaml:"buffer_size"`

	// BatchSize is the number of events to batch for replication
	BatchSize int `json:"batch_size" yaml:"batch_size"`

	// RetryConfig defines retry behavior for failed replications
	Retry RetryConfig `json:"retry" yaml:"retry"`

	// HealthCheck defines health check configuration
	HealthCheck HealthCheckConfig `json:"health_check" yaml:"health_check"`
}

// ReplicationRole defines the role of this datacenter
type ReplicationRole string

const (
	// RolePrimary means this DC accepts writes
	RolePrimary ReplicationRole = "primary"

	// RoleReplica means this DC is read-only and receives replicated data
	RoleReplica ReplicationRole = "replica"
)

// DatacenterTarget defines a remote datacenter to replicate to
type DatacenterTarget struct {
	// ID is the unique datacenter identifier
	ID string `json:"id" yaml:"id"`

	// Name is a human-readable name
	Name string `json:"name" yaml:"name"`

	// Endpoints are the gRPC endpoints for this datacenter
	Endpoints []string `json:"endpoints" yaml:"endpoints"`

	// Region is the geographic region
	Region string `json:"region" yaml:"region"`

	// TLS configuration
	TLSEnabled bool   `json:"tls_enabled" yaml:"tls_enabled"`
	TLSCertFile string `json:"tls_cert_file,omitempty" yaml:"tls_cert_file,omitempty"`
	TLSKeyFile  string `json:"tls_key_file,omitempty" yaml:"tls_key_file,omitempty"`
	TLSCAFile   string `json:"tls_ca_file,omitempty" yaml:"tls_ca_file,omitempty"`

	// Priority for failover (lower is higher priority)
	Priority int `json:"priority" yaml:"priority"`
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts
	MaxRetries int `json:"max_retries" yaml:"max_retries"`

	// InitialBackoff is the initial backoff duration
	InitialBackoff time.Duration `json:"initial_backoff" yaml:"initial_backoff"`

	// MaxBackoff is the maximum backoff duration
	MaxBackoff time.Duration `json:"max_backoff" yaml:"max_backoff"`

	// BackoffMultiplier is the multiplier for exponential backoff
	BackoffMultiplier float64 `json:"backoff_multiplier" yaml:"backoff_multiplier"`
}

// HealthCheckConfig defines health check settings
type HealthCheckConfig struct {
	// Enabled determines if health checks are active
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Interval is the time between health checks
	Interval time.Duration `json:"interval" yaml:"interval"`

	// Timeout is the health check timeout
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// FailureThreshold is the number of failures before marking unhealthy
	FailureThreshold int `json:"failure_threshold" yaml:"failure_threshold"`
}

// DefaultConfig returns a default replication configuration
func DefaultConfig() Config {
	return Config{
		Enabled:      false,
		DatacenterID: "dc-default",
		Role:         RolePrimary,
		BufferSize:   1000,
		BatchSize:    100,
		Retry: RetryConfig{
			MaxRetries:        5,
			InitialBackoff:    1 * time.Second,
			MaxBackoff:        30 * time.Second,
			BackoffMultiplier: 2.0,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:          true,
			Interval:         30 * time.Second,
			Timeout:          5 * time.Second,
			FailureThreshold: 3,
		},
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.DatacenterID == "" {
		return fmt.Errorf("datacenter_id is required")
	}

	if c.Role != RolePrimary && c.Role != RoleReplica {
		return fmt.Errorf("invalid role: %s (must be 'primary' or 'replica')", c.Role)
	}

	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be positive")
	}

	if c.BatchSize <= 0 {
		return fmt.Errorf("batch_size must be positive")
	}

	for i, target := range c.Targets {
		if err := target.Validate(); err != nil {
			return fmt.Errorf("target[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a datacenter target
func (t *DatacenterTarget) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("id is required")
	}

	if len(t.Endpoints) == 0 {
		return fmt.Errorf("at least one endpoint is required")
	}

	if t.TLSEnabled {
		if t.TLSCertFile == "" || t.TLSKeyFile == "" {
			return fmt.Errorf("tls_cert_file and tls_key_file are required when TLS is enabled")
		}
	}

	return nil
}

// IsPrimary returns true if this datacenter is the primary
func (c *Config) IsPrimary() bool {
	return c.Role == RolePrimary
}

// IsReplica returns true if this datacenter is a replica
func (c *Config) IsReplica() bool {
	return c.Role == RoleReplica
}
