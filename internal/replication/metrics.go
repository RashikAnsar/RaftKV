package replication

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics contains all replication-related Prometheus metrics
type Metrics struct {
	// Replication lag metrics
	LagSeconds *prometheus.GaugeVec
	LagEntries *prometheus.GaugeVec

	// Replication throughput
	EventsReplicated *prometheus.CounterVec
	EventsApplied    *prometheus.CounterVec
	EventsSkipped    *prometheus.CounterVec
	EventsDropped    *prometheus.CounterVec

	// Error metrics
	ReplicationErrors *prometheus.CounterVec
	ConnectionErrors  *prometheus.CounterVec

	// Health metrics
	HealthStatus      *prometheus.GaugeVec
	ConnectionStatus  *prometheus.GaugeVec
	ActiveWorkers     prometheus.Gauge
	ActiveStreams     prometheus.Gauge

	// Performance metrics
	ApplyDuration     *prometheus.HistogramVec
	StreamLatency     *prometheus.HistogramVec
	BatchSize         *prometheus.HistogramVec
}

// NewMetrics creates a new Metrics instance with all replication metrics
func NewMetrics(namespace string) *Metrics {
	if namespace == "" {
		namespace = "raftkv"
	}

	return &Metrics{
		// Lag metrics
		LagSeconds: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "lag_seconds",
				Help:      "Replication lag in seconds per target datacenter",
			},
			[]string{"datacenter_id", "datacenter_name"},
		),

		LagEntries: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "lag_entries",
				Help:      "Replication lag in number of entries per target datacenter",
			},
			[]string{"datacenter_id", "datacenter_name"},
		),

		// Throughput metrics
		EventsReplicated: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "events_replicated_total",
				Help:      "Total number of events replicated to remote datacenters",
			},
			[]string{"datacenter_id", "operation"},
		),

		EventsApplied: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "events_applied_total",
				Help:      "Total number of events applied from remote datacenters",
			},
			[]string{"source_datacenter_id", "operation"},
		),

		EventsSkipped: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "events_skipped_total",
				Help:      "Total number of events skipped (already applied or filtered)",
			},
			[]string{"source_datacenter_id", "reason"},
		),

		EventsDropped: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "events_dropped_total",
				Help:      "Total number of events dropped due to subscriber buffer overflow",
			},
			[]string{"datacenter_id"},
		),

		// Error metrics
		ReplicationErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "errors_total",
				Help:      "Total number of replication errors",
			},
			[]string{"datacenter_id", "error_type"},
		),

		ConnectionErrors: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "connection_errors_total",
				Help:      "Total number of connection errors to remote datacenters",
			},
			[]string{"datacenter_id", "error_type"},
		),

		// Health metrics
		HealthStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "health_status",
				Help:      "Health status of replication (0=unhealthy, 1=degraded, 2=healthy)",
			},
			[]string{"datacenter_id"},
		),

		ConnectionStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "connection_status",
				Help:      "Connection status to remote datacenters (0=disconnected, 1=connected)",
			},
			[]string{"datacenter_id", "datacenter_name"},
		),

		ActiveWorkers: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "active_workers",
				Help:      "Number of active replication workers",
			},
		),

		ActiveStreams: promauto.NewGauge(
			prometheus.GaugeOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "active_streams",
				Help:      "Number of active replication streams",
			},
		),

		// Performance metrics
		ApplyDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "apply_duration_seconds",
				Help:      "Time taken to apply a change event",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"operation"},
		),

		StreamLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "stream_latency_seconds",
				Help:      "Latency of streaming events to remote datacenters",
				Buckets:   prometheus.DefBuckets,
			},
			[]string{"datacenter_id"},
		),

		BatchSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Namespace: namespace,
				Subsystem: "replication",
				Name:      "batch_size",
				Help:      "Size of event batches being replicated",
				Buckets:   []float64{1, 10, 50, 100, 500, 1000},
			},
			[]string{"datacenter_id"},
		),
	}
}

// RecordLag updates lag metrics for a datacenter
func (m *Metrics) RecordLag(datacenterID, datacenterName string, lagSeconds float64, lagEntries uint64) {
	m.LagSeconds.WithLabelValues(datacenterID, datacenterName).Set(lagSeconds)
	m.LagEntries.WithLabelValues(datacenterID, datacenterName).Set(float64(lagEntries))
}

// RecordEventReplicated increments the replicated events counter
func (m *Metrics) RecordEventReplicated(datacenterID, operation string) {
	m.EventsReplicated.WithLabelValues(datacenterID, operation).Inc()
}

// RecordEventApplied increments the applied events counter
func (m *Metrics) RecordEventApplied(sourceDatacenterID, operation string) {
	m.EventsApplied.WithLabelValues(sourceDatacenterID, operation).Inc()
}

// RecordEventSkipped increments the skipped events counter
func (m *Metrics) RecordEventSkipped(sourceDatacenterID, reason string) {
	m.EventsSkipped.WithLabelValues(sourceDatacenterID, reason).Inc()
}

// RecordEventDropped increments the dropped events counter
func (m *Metrics) RecordEventDropped(datacenterID string) {
	m.EventsDropped.WithLabelValues(datacenterID).Inc()
}

// RecordReplicationError increments the replication error counter
func (m *Metrics) RecordReplicationError(datacenterID, errorType string) {
	m.ReplicationErrors.WithLabelValues(datacenterID, errorType).Inc()
}

// RecordConnectionError increments the connection error counter
func (m *Metrics) RecordConnectionError(datacenterID, errorType string) {
	m.ConnectionErrors.WithLabelValues(datacenterID, errorType).Inc()
}

// UpdateHealthStatus updates the health status gauge
// 0 = unhealthy, 1 = degraded, 2 = healthy
func (m *Metrics) UpdateHealthStatus(datacenterID string, status int) {
	m.HealthStatus.WithLabelValues(datacenterID).Set(float64(status))
}

// UpdateConnectionStatus updates the connection status gauge
// 0 = disconnected, 1 = connected
func (m *Metrics) UpdateConnectionStatus(datacenterID, datacenterName string, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	m.ConnectionStatus.WithLabelValues(datacenterID, datacenterName).Set(value)
}

// SetActiveWorkers sets the number of active workers
func (m *Metrics) SetActiveWorkers(count int) {
	m.ActiveWorkers.Set(float64(count))
}

// SetActiveStreams sets the number of active streams
func (m *Metrics) SetActiveStreams(count int) {
	m.ActiveStreams.Set(float64(count))
}

// ObserveApplyDuration records the duration of applying a change event
func (m *Metrics) ObserveApplyDuration(operation string, seconds float64) {
	m.ApplyDuration.WithLabelValues(operation).Observe(seconds)
}

// ObserveStreamLatency records the latency of streaming to a datacenter
func (m *Metrics) ObserveStreamLatency(datacenterID string, seconds float64) {
	m.StreamLatency.WithLabelValues(datacenterID).Observe(seconds)
}

// ObserveBatchSize records the size of a batch
func (m *Metrics) ObserveBatchSize(datacenterID string, size int) {
	m.BatchSize.WithLabelValues(datacenterID).Observe(float64(size))
}
