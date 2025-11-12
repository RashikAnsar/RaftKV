# RaftKV Operations Runbook

> Production operations guide for monitoring, backup, restore, and troubleshooting

## Table of Contents

- [Monitoring](#monitoring)
- [Backup & Restore](#backup--restore)
- [Performance Tuning](#performance-tuning)
- [Cluster Operations](#cluster-operations)
- [Troubleshooting](#troubleshooting)
- [Incident Response](#incident-response)
- [Maintenance Procedures](#maintenance-procedures)

---

## Monitoring

### Metrics Overview

RaftKV exposes **15+ Prometheus metrics** on the `/metrics` endpoint.

**Critical Metrics to Monitor:**

| Metric                                       | Description       | Alert Threshold         |
| -------------------------------------------- | ----------------- | ----------------------- |
| `kvstore_http_requests_total{status="5xx"}`  | Error rate        | >5% for 5 minutes       |
| `kvstore_storage_keys_total`                 | Current key count | N/A (capacity planning) |
| `raftkv_wal_size_bytes`                      | WAL disk usage    | >10GB                   |
| `raftkv_wal_segments_total`                  | WAL segment count | >100 segments           |
| `kvstore_storage_operation_duration_seconds` | Operation latency | P99 >100ms              |
| `raftkv_raft_is_leader`                      | Leader status     | No leader for >1 minute |

### Prometheus Setup

**Configuration:** `prometheus.yml`

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'raftkv'
    static_configs:
      - targets:
          - 'node1:8080'
          - 'node2:8080'
          - 'node3:8080'
    metrics_path: '/metrics'

alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - 'alertmanager:9093'

rule_files:
  - 'raftkv_alerts.yml'
```

**Start Prometheus:**

```bash
docker run -d \
  --name prometheus \
  --network raftkv-network \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  -v $(pwd)/raftkv_alerts.yml:/etc/prometheus/raftkv_alerts.yml \
  prom/prometheus:latest
```

### Alert Rules

**File:** `raftkv_alerts.yml`

```yaml
groups:
  - name: raftkv_critical
    interval: 30s
    rules:
      # No Raft Leader
      - alert: RaftNoLeader
        expr: absent(raftkv_raft_is_leader == 1)
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "No Raft leader elected"
          description: "RaftKV cluster has no leader for >1 minute. Writes are blocked."

      # High Error Rate
      - alert: HighErrorRate
        expr: |
          rate(kvstore_http_requests_total{status=~"5.."}[5m]) /
          rate(kvstore_http_requests_total[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High 5xx error rate ({{ $value | humanizePercentage }})"
          description: "Error rate >5% on {{ $labels.instance }}"

      # Disk Space Critical
      - alert: DiskSpaceCritical
        expr: raftkv_wal_size_bytes > 10737418240  # 10GB
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "WAL disk usage critical ({{ $value | humanize1024 }})"
          description: "WAL >10GB on {{ $labels.instance }}. Trigger snapshot."

      # Service Down
      - alert: ServiceDown
        expr: up{job="raftkv"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "RaftKV instance down"
          description: "{{ $labels.instance }} is not responding"

  - name: raftkv_warning
    interval: 1m
    rules:
      # High Write Latency
      - alert: HighWriteLatency
        expr: |
          histogram_quantile(0.99,
            kvstore_storage_operation_duration_seconds_bucket{operation="put"}
          ) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High write latency ({{ $value }}s)"
          description: "P99 write latency >100ms on {{ $labels.instance }}"

      # Low Cache Hit Rate
      - alert: LowCacheHitRate
        expr: |
          rate(kvstore_storage_operations_total{operation="get",status="cache_hit"}[5m]) /
          rate(kvstore_storage_operations_total{operation="get"}[5m]) < 0.2
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "Low cache hit rate ({{ $value | humanizePercentage }})"
          description: "Cache hit rate <20% on {{ $labels.instance }}"

      # High WAL Segment Count
      - alert: HighWALSegmentCount
        expr: raftkv_wal_segments_total > 100
        for: 15m
        labels:
          severity: warning
        annotations:
          summary: "High WAL segment count ({{ $value }})"
          description: "WAL has >100 segments on {{ $labels.instance }}. Check compaction."

      # Raft Log Growing
      - alert: RaftLogGrowing
        expr: raftkv_raft_log_entries_total > 50000
        for: 30m
        labels:
          severity: warning
        annotations:
          summary: "Raft log growing ({{ $value }} entries)"
          description: "Raft log >50K entries on {{ $labels.instance }}. Check snapshots."
```

### Grafana Dashboards

**Import Pre-built Dashboards:**

Location: `deployments/monitoring/grafana/dashboards/`

1. **Overview Dashboard** (`overview.json`)
   - Operations per second
   - Request latency (P50, P95, P99)
   - Error rate (5xx errors)
   - Key count over time
   - Cache hit rate

2. **Storage & Persistence Dashboard** (`storage.json`)
   - WAL segments count
   - WAL disk usage (MB)
   - Snapshot creation rate
   - Compaction metrics
   - Cache performance

3. **Cluster Health Dashboard** (`cluster.json`)
   - Raft leader status
   - Node states (Leader/Follower/Candidate)
   - Raft log entries
   - Cluster membership

**Import via Grafana UI:**

1. Login to Grafana: `http://localhost:3000` (admin/admin)
2. Add Prometheus data source:
   - URL: `http://prometheus:9090`
   - Access: Server (default)
3. Import dashboards:
   - Dashboard → Import
   - Upload JSON file or paste JSON
   - Select Prometheus data source

**Access Dashboards:**
- Overview: `http://localhost:3000/d/raftkv-overview`
- Storage: `http://localhost:3000/d/raftkv-storage`
- Cluster: `http://localhost:3000/d/raftkv-cluster`

### Key Queries

**Operations per Second:**
```promql
rate(kvstore_storage_operations_total[5m])
```

**P99 Latency:**
```promql
histogram_quantile(0.99,
  rate(kvstore_storage_operation_duration_seconds_bucket[5m])
)
```

**Error Rate:**
```promql
rate(kvstore_http_requests_total{status=~"5.."}[5m]) /
rate(kvstore_http_requests_total[5m])
```

**Cache Hit Rate:**
```promql
rate(kvstore_storage_operations_total{operation="get",status="cache_hit"}[5m]) /
rate(kvstore_storage_operations_total{operation="get"}[5m])
```

**WAL Disk Usage (MB):**
```promql
raftkv_wal_size_bytes / 1024 / 1024
```

---

## Backup & Restore

### Manual Snapshots

**Trigger Snapshot:**

```bash
# Via API (requires admin token)
curl -X POST http://localhost:8080/admin/snapshot \
  -H "Authorization: Bearer $TOKEN"

# Response:
{
  "snapshot": "/var/lib/raftkv/snapshots/snapshot-12345.gob",
  "message": "snapshot created successfully"
}
```

**Automatic Snapshots:**

Configured via `storage.snapshot_every` (default: 10,000 operations).

```yaml
storage:
  snapshot_every: 10000  # Snapshot every 10K operations
```

### Backup Procedures

#### Single-Node Backup

**Option 1: Snapshot + WAL Backup**

```bash
#!/bin/bash
# backup-raftkv.sh

BACKUP_DIR="/backup/raftkv/$(date +%Y%m%d-%H%M%S)"
DATA_DIR="/var/lib/raftkv"

# Create backup directory
mkdir -p $BACKUP_DIR

# Trigger snapshot
curl -X POST http://localhost:8080/admin/snapshot \
  -H "Authorization: Bearer $TOKEN"

# Wait for snapshot to complete
sleep 5

# Copy snapshots
cp -r $DATA_DIR/snapshots $BACKUP_DIR/

# Copy WAL (optional, for point-in-time recovery)
cp $DATA_DIR/*.wal $BACKUP_DIR/ 2>/dev/null || true

# Compress
tar -czf $BACKUP_DIR.tar.gz -C $(dirname $BACKUP_DIR) $(basename $BACKUP_DIR)
rm -rf $BACKUP_DIR

echo "Backup created: $BACKUP_DIR.tar.gz"
```

**Option 2: Full Data Directory Backup**

```bash
#!/bin/bash
# Stop service
sudo systemctl stop raftkv

# Backup entire data directory
tar -czf /backup/raftkv-full-$(date +%Y%m%d).tar.gz /var/lib/raftkv

# Restart service
sudo systemctl start raftkv
```

#### Cluster Backup

**Best Practice:** Backup from a follower node to avoid impacting the leader.

```bash
# Identify follower
FOLLOWER=$(curl -s http://node1:8080/cluster/nodes | \
  jq -r '.nodes[] | select(.id != "node1") | .id' | head -1)

# Trigger snapshot on follower
curl -X POST http://$FOLLOWER:8080/admin/snapshot \
  -H "Authorization: Bearer $TOKEN"

# Copy snapshots
scp -r $FOLLOWER:/var/lib/raftkv/snapshots /backup/
```

#### Automated Backup (Cron)

```bash
# Add to crontab
crontab -e

# Backup every 6 hours
0 */6 * * * /usr/local/bin/backup-raftkv.sh

# Backup daily at 2 AM
0 2 * * * /usr/local/bin/backup-raftkv.sh
```

### Restore Procedures

#### Restore from Snapshot

**Single-Node Restore:**

```bash
#!/bin/bash
# restore-raftkv.sh

BACKUP_FILE="/backup/raftkv-20250109.tar.gz"
DATA_DIR="/var/lib/raftkv"

# Stop service
sudo systemctl stop raftkv

# Backup current state (just in case)
mv $DATA_DIR $DATA_DIR.backup.$(date +%Y%m%d-%H%M%S)

# Extract backup
mkdir -p $DATA_DIR
tar -xzf $BACKUP_FILE -C /tmp/
cp -r /tmp/raftkv-*/snapshots $DATA_DIR/

# Remove WAL (will replay from snapshot)
rm -f $DATA_DIR/*.wal

# Fix permissions
chown -R raftkv:raftkv $DATA_DIR

# Start service
sudo systemctl start raftkv

# Verify
curl http://localhost:8080/health
```

#### Cluster Restore

**Restore All Nodes:**

```bash
# Stop all nodes
for node in node1 node2 node3; do
  ssh $node "sudo systemctl stop raftkv"
done

# Restore snapshot to all nodes
for node in node1 node2 node3; do
  scp -r /backup/snapshots $node:/var/lib/raftkv/
  ssh $node "sudo rm -f /var/lib/raftkv/*.wal"
  ssh $node "sudo rm -rf /var/lib/raftkv/raft"
  ssh $node "sudo chown -R raftkv:raftkv /var/lib/raftkv"
done

# Start bootstrap node first
ssh node1 "sudo systemctl start raftkv"
sleep 10

# Start follower nodes
for node in node2 node3; do
  ssh $node "sudo systemctl start raftkv"
done

# Verify cluster
curl http://node1:8080/cluster/nodes
```

### Disaster Recovery

**Complete Cluster Loss:**

1. **Restore from backup on node1 with bootstrap=true**
2. **Remove Raft state:** `rm -rf /var/lib/raftkv/raft`
3. **Start node1** (will become leader)
4. **Join node2 and node3** as new members

```bash
# Node1: Bootstrap with restored data
sudo systemctl start raftkv

# Node2 & Node3: Join as new members
# (Raft state will be replicated from leader)
```

---

## Performance Tuning

### Identify Performance Bottlenecks

**Check Current Performance:**

```bash
# Get statistics
curl http://localhost:8080/stats | jq

# Check metrics
curl http://localhost:8080/metrics | grep duration
```

**Common Bottlenecks:**

1. **Disk I/O** - Slow writes, high fsync latency
2. **Network** - High inter-node latency (cluster mode)
3. **CPU** - High CPU usage during compaction
4. **Memory** - Low cache hit rate, frequent evictions

### Tuning Parameters

#### WAL Batching

**Problem:** High write latency, low throughput

**Solution:** Enable and tune batching

```yaml
wal:
  batch_enabled: true
  batch_size: 100           # Operations per batch
  batch_wait_time: 10ms     # Max wait time
  batch_bytes: 1048576      # 1MB max batch size
```

**Effects:**
- ✅ **10x throughput improvement** (20K → 200K ops/sec)
- ⚠️ Slight increase in write latency (+10ms)

**Tuning Guidelines:**
- **Low latency priority**: `batch_wait_time: 5ms`
- **High throughput priority**: `batch_size: 1000`, `batch_wait_time: 50ms`
- **Balanced**: Default values

#### LRU Cache

**Problem:** Low cache hit rate, slow reads

**Solution:** Increase cache size

```yaml
cache:
  enabled: true
  max_size: 10000           # Increase for better hit rate
  ttl: 0                    # No expiration (or set TTL)
```

**Effects:**
- ✅ **5x faster reads** for cached keys
- ⚠️ Increased memory usage (~100MB per 10K entries)

**Tuning Guidelines:**
- **Read-heavy workload**: `max_size: 100000` (more cache)
- **Write-heavy workload**: `max_size: 1000` (less cache)
- **Monitor**: `cache_hit_rate` metric

**Calculate Cache Size:**

```bash
# Current hit rate
curl http://localhost:8080/stats | jq '.cache.hit_rate'

# If hit rate <20%, increase cache size
# If hit rate >90%, cache is sufficient
```

#### Snapshot Frequency

**Problem:** High WAL disk usage, slow recovery

**Solution:** Tune snapshot interval

```yaml
storage:
  snapshot_every: 10000     # Snapshot every 10K ops
```

**Effects:**
- ✅ Faster recovery (less WAL replay)
- ✅ Lower WAL disk usage
- ⚠️ More frequent compaction (CPU overhead)

**Tuning Guidelines:**
- **High write rate**: `snapshot_every: 5000` (more frequent)
- **Low write rate**: `snapshot_every: 50000` (less frequent)
- **Monitor**: `raftkv_wal_segments_total` metric

#### WAL Compaction

**Problem:** WAL growing unbounded

**Solution:** Enable compaction

```yaml
wal:
  compaction_enabled: true
  compaction_margin: 100    # Keep 100 entries before snapshot
```

**Effects:**
- ✅ Bounded WAL disk usage
- ⚠️ CPU overhead during compaction

#### Resource Limits

**Systemd Service:**

```ini
[Service]
# Memory limit
MemoryLimit=4G
MemoryHigh=3G

# CPU limit
CPUQuota=200%  # 2 cores

# File descriptor limit
LimitNOFILE=65536

# Process limit
LimitNPROC=4096
```

### Benchmarking

**Run Benchmarks:**

```bash
# Build benchmark tool
make bench

# Run all benchmarks
go test -bench=. -benchmem ./test/benchmark/

# Run specific benchmark
go test -bench=BenchmarkBatchedWrites -benchmem ./test/benchmark/

# Profile CPU
go test -bench=. -cpuprofile=cpu.out ./test/benchmark/
go tool pprof cpu.out

# Profile memory
go test -bench=. -memprofile=mem.out ./test/benchmark/
go tool pprof mem.out
```

**Expected Performance:**

| Operation       | Single-Node   | 3-Node Cluster |
| --------------- | ------------- | -------------- |
| Write (batched) | ~200K ops/sec | ~50K ops/sec   |
| Read (cached)   | ~500K ops/sec | ~200K ops/sec  |
| Read (uncached) | ~200K ops/sec | ~200K ops/sec  |

---

## Cluster Operations

### Add Node to Cluster

**Scenario:** Scale from 3 nodes to 5 nodes

**Procedure:**

```bash
# 1. Prepare new node (node4)
ssh node4

# 2. Install RaftKV
sudo cp kvstore /usr/local/bin/
sudo mkdir -p /var/lib/raftkv

# 3. Create configuration
cat <<EOF | sudo tee /etc/raftkv/config.yaml
raft:
  enabled: true
  node_id: "node4"
  raft_addr: "node4:7000"
  join_addr: "http://node1:8080/cluster/join"

# ... other config ...
EOF

# 4. Start service
sudo systemctl start raftkv

# 5. Verify join
curl http://node1:8080/cluster/nodes
```

**Expected Output:**
```json
{
  "nodes": [
    {"id": "node1", "address": "node1:7000", "suffrage": "Voter"},
    {"id": "node2", "address": "node2:7000", "suffrage": "Voter"},
    {"id": "node3", "address": "node3:7000", "suffrage": "Voter"},
    {"id": "node4", "address": "node4:7000", "suffrage": "Voter"}
  ]
}
```

### Remove Node from Cluster

**Scenario:** Decommission node3

**Procedure:**

```bash
# 1. Remove from cluster (from leader)
curl -X POST http://node1:8080/cluster/remove \
  -H "Content-Type: application/json" \
  -d '{"node_id": "node3"}'

# 2. Stop service on node3
ssh node3 "sudo systemctl stop raftkv"

# 3. Verify removal
curl http://node1:8080/cluster/nodes

# 4. Clean up data (optional)
ssh node3 "sudo rm -rf /var/lib/raftkv"
```

### Leader Election

**Manual Leader Step-Down (for maintenance):**

```bash
# 1. Identify current leader
LEADER=$(curl -s http://node1:8080/cluster/leader | jq -r '.leader')

# 2. Stop leader node
ssh $LEADER "sudo systemctl stop raftkv"

# 3. Wait for new leader election (<1 second)
sleep 2

# 4. Verify new leader
curl http://node2:8080/cluster/leader
```

### Cluster Health Check

```bash
#!/bin/bash
# cluster-health-check.sh

NODES=("node1:8080" "node2:8080" "node3:8080")

echo "=== Cluster Health Check ==="
echo

for node in "${NODES[@]}"; do
  echo "Node: $node"

  # Health
  STATUS=$(curl -s http://$node/health | jq -r '.status')
  echo "  Status: $STATUS"

  # Role
  ROLE=$(curl -s http://$node/cluster/leader | jq -r '.state')
  echo "  Role: $ROLE"

  # Key count
  KEYS=$(curl -s http://$node/stats | jq -r '.store.key_count')
  echo "  Keys: $KEYS"

  echo
done

# Leader
LEADER=$(curl -s http://node1:8080/cluster/leader | jq -r '.leader')
echo "Current Leader: $LEADER"
```

---

## Troubleshooting

### Common Issues

#### Issue: High Write Latency

**Symptoms:**
- P99 write latency >100ms
- Slow API responses for PUT/DELETE

**Diagnosis:**

```bash
# Check latency
curl http://localhost:8080/metrics | grep operation_duration

# Check disk I/O
iostat -x 1 10

# Check WAL size
ls -lh /var/lib/raftkv/*.wal
```

**Solutions:**

1. **Enable batching:**
   ```yaml
   wal:
     batch_enabled: true
   ```

2. **Use faster disk (SSD):**
   - Migrate to SSD storage
   - Increase IOPS (cloud)

3. **Reduce snapshot frequency:**
   ```yaml
   storage:
     snapshot_every: 20000
   ```

4. **Check disk health:**
   ```bash
   sudo smartctl -a /dev/sda
   ```

---

#### Issue: Low Cache Hit Rate

**Symptoms:**
- Cache hit rate <20%
- Slow read operations

**Diagnosis:**

```bash
# Check hit rate
curl http://localhost:8080/stats | jq '.cache.hit_rate'

# Check evictions
curl http://localhost:8080/stats | jq '.cache.evictions'
```

**Solutions:**

1. **Increase cache size:**
   ```yaml
   cache:
     max_size: 100000
   ```

2. **Add TTL (if stale data acceptable):**
   ```yaml
   cache:
     ttl: 1h
   ```

3. **Analyze access patterns:**
   - Are keys accessed randomly or repeatedly?
   - Consider application-level caching

---

#### Issue: WAL Growing Unbounded

**Symptoms:**
- Disk usage increasing
- `raftkv_wal_segments_total` >100

**Diagnosis:**

```bash
# Check WAL size
du -sh /var/lib/raftkv/*.wal

# Check segment count
ls /var/lib/raftkv/*.wal | wc -l

# Check compaction status
curl http://localhost:8080/metrics | grep compaction
```

**Solutions:**

1. **Enable compaction:**
   ```yaml
   wal:
     compaction_enabled: true
   ```

2. **Trigger manual snapshot:**
   ```bash
   curl -X POST http://localhost:8080/admin/snapshot \
     -H "Authorization: Bearer $TOKEN"
   ```

3. **Reduce snapshot interval:**
   ```yaml
   storage:
     snapshot_every: 5000
   ```

4. **Check snapshot creation:**
   ```bash
   sudo journalctl -u raftkv | grep snapshot
   ```

---

#### Issue: No Raft Leader Elected

**Symptoms:**
- Writes fail with "no leader"
- `raftkv_raft_is_leader` metric absent

**Diagnosis:**

```bash
# Check cluster nodes
curl http://node1:8080/cluster/nodes

# Check leader status on all nodes
for node in node1 node2 node3; do
  echo "=== $node ==="
  curl http://$node:8080/cluster/leader
done

# Check Raft logs
sudo journalctl -u raftkv | grep -i "leader\|election\|vote"
```

**Solutions:**

1. **Network partition:**
   - Check network connectivity between nodes
   - Verify firewall rules (port 7000)
   ```bash
   ping node2
   telnet node2 7000
   ```

2. **Insufficient nodes:**
   - Raft requires majority (2/3 for 3-node cluster)
   - Ensure at least 2 nodes are running

3. **Clock skew:**
   - Check system clocks on all nodes
   ```bash
   ssh node1 "date"
   ssh node2 "date"
   ssh node3 "date"
   ```

4. **Restart cluster:**
   ```bash
   # Stop all nodes
   for node in node1 node2 node3; do
     ssh $node "sudo systemctl stop raftkv"
   done

   # Start bootstrap node first
   ssh node1 "sudo systemctl start raftkv"
   sleep 10

   # Start followers
   for node in node2 node3; do
     ssh $node "sudo systemctl start raftkv"
   done
   ```

---

#### Issue: Service Won't Start

**Symptoms:**
- `systemctl start raftkv` fails
- Service immediately exits

**Diagnosis:**

```bash
# Check service status
sudo systemctl status raftkv

# Check logs
sudo journalctl -u raftkv -n 100 --no-pager

# Check configuration
raftkv --config /etc/raftkv/config.yaml --validate
```

**Common Causes:**

1. **Port already in use:**
   ```bash
   sudo netstat -tulpn | grep 8080
   sudo lsof -i :8080
   ```

   **Solution:** Change port or kill conflicting process

2. **Permission denied:**
   ```bash
   ls -la /var/lib/raftkv
   ls -la /etc/raftkv/config.yaml
   ```

   **Solution:** Fix ownership
   ```bash
   sudo chown -R raftkv:raftkv /var/lib/raftkv
   ```

3. **Invalid configuration:**
   ```bash
   # Validate YAML syntax
   yamllint /etc/raftkv/config.yaml
   ```

4. **Missing JWT secret:**
   ```bash
   # Check environment
   sudo systemctl show raftkv | grep RAFTKV_JWT_SECRET
   ```

---

## Incident Response

### Incident Severity Levels

| Level             | Description      | Response Time | Examples                          |
| ----------------- | ---------------- | ------------- | --------------------------------- |
| **P1 - Critical** | Complete outage  | Immediate     | All nodes down, data corruption   |
| **P2 - High**     | Partial outage   | <15 minutes   | Leader down, majority unavailable |
| **P3 - Medium**   | Degraded service | <1 hour       | High latency, follower down       |
| **P4 - Low**      | Minor issue      | <4 hours      | Non-critical warnings             |

### Incident Response Playbook

#### P1: Complete Cluster Outage

**Symptoms:** All nodes down, no leader

**Steps:**

1. **Assess scope:**
   ```bash
   # Check all nodes
   for node in node1 node2 node3; do
     curl -m 2 http://$node:8080/health || echo "$node DOWN"
   done
   ```

2. **Check infrastructure:**
   - Network connectivity
   - Power/hardware status
   - Cloud provider status

3. **Restart nodes:**
   ```bash
   # Start bootstrap node first
   ssh node1 "sudo systemctl start raftkv"
   sleep 10

   # Start followers
   ssh node2 "sudo systemctl start raftkv"
   ssh node3 "sudo systemctl start raftkv"
   ```

4. **Verify recovery:**
   ```bash
   curl http://node1:8080/cluster/nodes
   curl http://node1:8080/stats
   ```

5. **If data corruption:**
   - Restore from backup (see [Restore Procedures](#restore-procedures))

---

#### P2: Leader Down

**Symptoms:** Leader node unavailable, no writes possible

**Steps:**

1. **Wait for leader election** (<1 second)
   ```bash
   sleep 2
   curl http://node2:8080/cluster/leader
   ```

2. **If no new leader elected:**
   - Check network connectivity
   - Ensure majority nodes (2/3) are available
   - Check logs for split-brain

3. **Recover failed node:**
   ```bash
   ssh failed-node "sudo systemctl restart raftkv"
   ```

---

### On-Call Runbook

**Critical Alerts:**

| Alert             | Severity | Action                                |
| ----------------- | -------- | ------------------------------------- |
| RaftNoLeader      | P1       | Follow "P1: Complete Cluster Outage"  |
| HighErrorRate     | P2       | Check logs, restart service if needed |
| DiskSpaceCritical | P2       | Trigger snapshot, clean up WAL        |
| ServiceDown       | P2       | Restart service, check logs           |

**Escalation Path:**

1. **Level 1:** On-call engineer (15 minutes)
2. **Level 2:** Senior engineer (30 minutes)
3. **Level 3:** Engineering manager (1 hour)

---

## Maintenance Procedures

### Rolling Upgrade (Zero-Downtime)

**Scenario:** Upgrade from v1.0.0 to v1.1.0

**Prerequisites:**
- Version compatibility verified
- Backup taken

**Procedure:**

```bash
# 1. Upgrade follower nodes first (one at a time)
ssh node2

# Stop service
sudo systemctl stop raftkv

# Backup old binary
sudo cp /usr/local/bin/kvstore /usr/local/bin/kvstore.v1.0.0

# Install new binary
sudo cp kvstore-v1.1.0 /usr/local/bin/kvstore

# Start service
sudo systemctl start raftkv

# Verify
curl http://node2:8080/health

# Repeat for node3
ssh node3
# ... same steps ...

# 2. Upgrade leader last
ssh node1

# Step down leader (optional)
# New leader will be elected from already-upgraded followers

# Stop service
sudo systemctl stop raftkv

# Install new binary
sudo cp kvstore-v1.1.0 /usr/local/bin/kvstore

# Start service
sudo systemctl start raftkv

# 3. Verify cluster
curl http://node1:8080/cluster/nodes
curl http://node1:8080/stats
```

**Rollback Procedure:**

If issues detected:

```bash
# Stop service
sudo systemctl stop raftkv

# Restore old binary
sudo cp /usr/local/bin/kvstore.v1.0.0 /usr/local/bin/kvstore

# Start service
sudo systemctl start raftkv
```

---

### Log Rotation

**journald (default):**

```bash
# Configure max size
sudo nano /etc/systemd/journald.conf

# Set limits
SystemMaxUse=1G
SystemKeepFree=500M
MaxRetentionSec=7days

# Restart journald
sudo systemctl restart systemd-journald
```

**Manual Cleanup:**

```bash
# View log size
sudo journalctl --disk-usage

# Clean logs older than 7 days
sudo journalctl --vacuum-time=7d

# Clean logs larger than 500MB
sudo journalctl --vacuum-size=500M
```

---

### Regular Maintenance Tasks

**Daily:**
- [ ] Check alert dashboard
- [ ] Review error logs
- [ ] Monitor disk usage

**Weekly:**
- [ ] Review performance metrics
- [ ] Check backup completion
- [ ] Verify snapshot creation

**Monthly:**
- [ ] Review access logs
- [ ] Rotate API keys
- [ ] Update documentation
- [ ] Test restore procedure
- [ ] Review capacity planning

**Quarterly:**
- [ ] Security audit
- [ ] Performance review
- [ ] Disaster recovery drill
- [ ] Update runbooks

---

## Further Reading

- [API_REFERENCE.md](API_REFERENCE.md) - Complete API documentation
- [DEPLOYMENT.md](DEPLOYMENT.md) - Deployment procedures
- [ARCHITECTURE.md](ARCHITECTURE.md) - System architecture
- [Prometheus Documentation](https://prometheus.io/docs/)
- [Grafana Documentation](https://grafana.com/docs/)
