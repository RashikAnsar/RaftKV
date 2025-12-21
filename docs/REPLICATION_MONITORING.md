# Replication Monitoring Guide

**Last Updated:** 2025-12-06
**Status:** Phase 3C Complete

---

## Overview

This guide describes the Prometheus metrics and Grafana dashboards available for monitoring Multi-DC replication in RaftKV.

---

## Prometheus Metrics

### Lag Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `raftkv_replication_lag_seconds` | Gauge | `datacenter_id`, `datacenter_name` | Replication lag in seconds |
| `raftkv_replication_lag_entries` | Gauge | `datacenter_id`, `datacenter_name` | Replication lag in number of entries |

**Usage:**
```promql
# Current lag to all datacenters
raftkv_replication_lag_seconds

# Lag exceeding 10 seconds
raftkv_replication_lag_seconds > 10

# Average lag over 5 minutes
avg_over_time(raftkv_replication_lag_seconds[5m])
```

---

### Throughput Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `raftkv_replication_events_replicated_total` | Counter | `datacenter_id`, `operation` | Total events replicated |
| `raftkv_replication_events_applied_total` | Counter | `source_datacenter_id`, `operation` | Total events applied |
| `raftkv_replication_events_skipped_total` | Counter | `source_datacenter_id`, `reason` | Total events skipped |
| `raftkv_replication_events_dropped_total` | Counter | `datacenter_id` | Total events dropped |

**Usage:**
```promql
# Replication rate (events/sec)
rate(raftkv_replication_events_replicated_total[1m])

# Total events replicated to dc-replica-1
sum(raftkv_replication_events_replicated_total{datacenter_id="dc-replica-1"})

# Events skipped due to idempotency
sum(raftkv_replication_events_skipped_total{reason="already_applied"})
```

---

### Error Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `raftkv_replication_errors_total` | Counter | `datacenter_id`, `error_type` | Total replication errors |
| `raftkv_replication_connection_errors_total` | Counter | `datacenter_id`, `error_type` | Total connection errors |

**Usage:**
```promql
# Error rate
rate(raftkv_replication_errors_total[5m])

# Connection errors per DC
sum by (datacenter_id) (raftkv_replication_connection_errors_total)
```

---

### Health Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `raftkv_replication_health_status` | Gauge | `datacenter_id` | Health status (0=unhealthy, 1=degraded, 2=healthy) |
| `raftkv_replication_connection_status` | Gauge | `datacenter_id`, `datacenter_name` | Connection status (0=disconnected, 1=connected) |
| `raftkv_replication_active_workers` | Gauge | - | Number of active replication workers |
| `raftkv_replication_active_streams` | Gauge | - | Number of active replication streams |

**Usage:**
```promql
# Unhealthy datacenters
raftkv_replication_health_status < 2

# Disconnected datacenters
raftkv_replication_connection_status == 0

# Number of active workers
raftkv_replication_active_workers
```

---

### Performance Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `raftkv_replication_apply_duration_seconds` | Histogram | `operation` | Time to apply change events |
| `raftkv_replication_stream_latency_seconds` | Histogram | `datacenter_id` | Streaming latency |
| `raftkv_replication_batch_size` | Histogram | `datacenter_id` | Event batch sizes |

**Usage:**
```promql
# 95th percentile apply duration
histogram_quantile(0.95, raftkv_replication_apply_duration_seconds_bucket)

# Average batch size
avg(raftkv_replication_batch_size)
```

---

## Grafana Dashboard

### Recommended Panels

#### 1. Replication Lag
```
Panel Type: Graph
Query: raftkv_replication_lag_seconds
Legend: {{datacenter_name}}
Unit: seconds
Threshold: Warning at 5s, Critical at 10s
```

#### 2. Replication Throughput
```
Panel Type: Graph
Query: rate(raftkv_replication_events_replicated_total[1m])
Legend: {{datacenter_id}} - {{operation}}
Unit: events/sec
```

#### 3. Health Status
```
Panel Type: Stat
Query: raftkv_replication_health_status
Mappings:
  0 → Unhealthy (red)
  1 → Degraded (yellow)
  2 → Healthy (green)
```

#### 4. Connection Status
```
Panel Type: Stat
Query: raftkv_replication_connection_status
Mappings:
  0 → Disconnected (red)
  1 → Connected (green)
```

#### 5. Error Rate
```
Panel Type: Graph
Query: rate(raftkv_replication_errors_total[5m])
Legend: {{datacenter_id}} - {{error_type}}
Unit: errors/sec
```

#### 6. Apply Duration (p95)
```
Panel Type: Graph
Query: histogram_quantile(0.95, raftkv_replication_apply_duration_seconds_bucket)
Legend: p95 apply duration
Unit: seconds
```

---

## Alerting Rules

### Critical Alerts

#### High Replication Lag
```yaml
alert: HighReplicationLag
expr: raftkv_replication_lag_seconds > 10
for: 5m
labels:
  severity: critical
annotations:
  summary: "High replication lag to {{ $labels.datacenter_name }}"
  description: "Replication lag is {{ $value }}s (threshold: 10s)"
```

#### Datacenter Disconnected
```yaml
alert: DatacenterDisconnected
expr: raftkv_replication_connection_status == 0
for: 2m
labels:
  severity: critical
annotations:
  summary: "Datacenter {{ $labels.datacenter_name }} disconnected"
  description: "Connection to {{ $labels.datacenter_id }} has been down for 2 minutes"
```

#### Replication Unhealthy
```yaml
alert: ReplicationUnhealthy
expr: raftkv_replication_health_status < 2
for: 3m
labels:
  severity: warning
annotations:
  summary: "Replication to {{ $labels.datacenter_id }} is unhealthy"
  description: "Health status: {{ $value }} (0=unhealthy, 1=degraded, 2=healthy)"
```

### Warning Alerts

#### High Error Rate
```yaml
alert: HighReplicationErrorRate
expr: rate(raftkv_replication_errors_total[5m]) > 1
for: 5m
labels:
  severity: warning
annotations:
  summary: "High replication error rate to {{ $labels.datacenter_id }}"
  description: "Error rate: {{ $value }} errors/sec"
```

#### Events Being Dropped
```yaml
alert: ReplicationEventsDropped
expr: increase(raftkv_replication_events_dropped_total[5m]) > 10
for: 2m
labels:
  severity: warning
annotations:
  summary: "Replication events being dropped for {{ $labels.datacenter_id }}"
  description: "{{ $value }} events dropped in last 5 minutes (buffer overflow)"
```

---

## Monitoring Best Practices

### 1. Set Up Lag Alerts
- **Warning:** Lag > 5 seconds for 3 minutes
- **Critical:** Lag > 10 seconds for 5 minutes

### 2. Monitor Connection Health
- Alert immediately on disconnections
- Track connection error patterns

### 3. Watch Error Rates
- Alert on sustained error rates
- Investigate error types

### 4. Track Throughput
- Monitor events/sec per datacenter
- Compare with expected load

### 5. Performance Metrics
- Monitor p95/p99 apply duration
- Watch for degradation trends

---

## Troubleshooting

### High Lag

**Symptoms:**
- `raftkv_replication_lag_seconds` > threshold
- `raftkv_replication_lag_entries` increasing

**Possible Causes:**
1. Network issues (check `raftkv_replication_stream_latency_seconds`)
2. Remote DC overloaded (check remote DC metrics)
3. Slow apply operations (check `raftkv_replication_apply_duration_seconds`)

**Solutions:**
- Increase batch size for better throughput
- Check network connectivity
- Scale up remote DC resources

### Events Being Dropped

**Symptoms:**
- `raftkv_replication_events_dropped_total` increasing

**Causes:**
- Subscriber buffer overflow
- Remote DC too slow to consume

**Solutions:**
- Increase `buffer_size` in config
- Investigate slow remote DC
- Add more replicas to distribute load

### Connection Failures

**Symptoms:**
- `raftkv_replication_connection_status` = 0
- `raftkv_replication_connection_errors_total` increasing

**Causes:**
- Network partition
- Remote DC down
- TLS certificate issues

**Solutions:**
- Check network connectivity
- Verify remote DC health
- Check TLS certificate validity

---

## Example PromQL Queries

### Overall Health Dashboard
```promql
# Total replication throughput
sum(rate(raftkv_replication_events_replicated_total[1m]))

# Average lag across all DCs
avg(raftkv_replication_lag_seconds)

# Number of healthy DCs
count(raftkv_replication_health_status == 2)

# Total error rate
sum(rate(raftkv_replication_errors_total[5m]))
```

### Per-Datacenter Dashboard
```promql
# Lag for specific DC
raftkv_replication_lag_seconds{datacenter_id="dc-replica-1"}

# Events/sec to specific DC
rate(raftkv_replication_events_replicated_total{datacenter_id="dc-replica-1"}[1m])

# Connection status
raftkv_replication_connection_status{datacenter_id="dc-replica-1"}
```

---

## Capacity Planning

Use these metrics for capacity planning:

1. **Peak Throughput:** `max_over_time(rate(raftkv_replication_events_replicated_total[1m])[1d])`
2. **Average Lag:** `avg_over_time(raftkv_replication_lag_seconds[1d])`
3. **Event Drop Rate:** `rate(raftkv_replication_events_dropped_total[1h])`

If you see:
- Sustained high throughput → Plan for more bandwidth
- Increasing lag → Scale up replicas or optimize apply logic
- Events dropping → Increase buffer sizes

---

## Summary

**Key Metrics to Monitor:**
- ✅ Replication lag (seconds & entries)
- ✅ Throughput (events/sec)
- ✅ Health status
- ✅ Connection status
- ✅ Error rates

**Critical Alerts:**
- High lag (> 10s for 5min)
- Datacenter disconnected (> 2min)
- Replication unhealthy (> 3min)

**For detailed implementation, see:**
- [metrics.go](../internal/replication/metrics.go) - Metrics definitions
- [coordinator.go](../internal/replication/coordinator.go) - Metrics integration
- [lag_monitor.go](../internal/replication/lag_monitor.go) - Lag tracking
