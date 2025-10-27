package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	HTTPRequestsTotal   *prometheus.CounterVec
	HTTPRequestDuration *prometheus.HistogramVec
	HTTPRequestSize     *prometheus.HistogramVec
	HTTPResponseSize    *prometheus.HistogramVec

	StorageOperationsTotal   *prometheus.CounterVec
	StorageOperationDuration *prometheus.HistogramVec
	StorageKeysTotal         prometheus.Gauge
	StorageSnapshotsTotal    prometheus.Gauge
}

func NewMetrics() *Metrics {
	m := &Metrics{
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kvstore_http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "endpoint", "status"},
		),

		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kvstore_http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: prometheus.DefBuckets, // Default: .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10
			},
			[]string{"method", "endpoint"},
		),

		HTTPRequestSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kvstore_http_request_size_bytes",
				Help:    "HTTP request size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 6), // 100, 1K, 10K, 100K, 1M, 10M
			},
			[]string{"method", "endpoint"},
		),

		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kvstore_http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: prometheus.ExponentialBuckets(100, 10, 6),
			},
			[]string{"method", "endpoint"},
		),

		StorageOperationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kvstore_storage_operations_total",
				Help: "Total number of storage operations",
			},
			[]string{"operation", "status"},
		),

		StorageOperationDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kvstore_storage_operation_duration_seconds",
				Help:    "Storage operation duration in seconds",
				Buckets: []float64{.00001, .00005, .0001, .0005, .001, .005, .01, .05, .1, .5, 1}, // Microsecond to second scale
			},
			[]string{"operation"},
		),

		StorageKeysTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "kvstore_storage_keys_total",
				Help: "Current number of keys in storage",
			},
		),

		StorageSnapshotsTotal: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "kvstore_storage_snapshots_total",
				Help: "Current number of snapshots",
			},
		),
	}

	return m
}

func (m *Metrics) RecordHTTPRequest(method, endpoint, status string, duration float64, requestSize, responseSize int) {
	m.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, endpoint).Observe(duration)
	m.HTTPRequestSize.WithLabelValues(method, endpoint).Observe(float64(requestSize))
	m.HTTPResponseSize.WithLabelValues(method, endpoint).Observe(float64(responseSize))
}

func (m *Metrics) RecordStorageOperation(operation, status string, duration float64) {
	m.StorageOperationsTotal.WithLabelValues(operation, status).Inc()
	m.StorageOperationDuration.WithLabelValues(operation).Observe(duration)
}

func (m *Metrics) UpdateStorageMetrics(keyCount, snapshotCount int64) {
	m.StorageKeysTotal.Set(float64(keyCount))
	m.StorageSnapshotsTotal.Set(float64(snapshotCount))
}
