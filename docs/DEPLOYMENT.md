# RaftKV Deployment Guide

> Complete guide for deploying RaftKV in production

## Table of Contents

- [Prerequisites](#prerequisites)
- [Single-Node Deployment](#single-node-deployment)
- [3-Node Cluster Deployment](#3-node-cluster-deployment)
- [Docker Deployment](#docker-deployment)
- [TLS/mTLS Setup](#tlsmtls-setup)
- [Authentication Setup](#authentication-setup)
- [Monitoring Setup](#monitoring-setup)
- [Production Checklist](#production-checklist)
- [Troubleshooting](#troubleshooting)

---

## Prerequisites

### System Requirements

**Minimum (Development):**
- CPU: 1 core
- RAM: 512MB
- Disk: 1GB SSD
- OS: Linux, macOS, or Windows

**Recommended (Production):**
- CPU: 4 cores
- RAM: 4GB
- Disk: 50GB SSD (IOPS: 3000+)
- OS: Linux (Ubuntu 20.04+, RHEL 8+, or Debian 11+)

### Software Requirements

- **Go 1.21+** (for building from source)
- **systemd** (for service management on Linux)
- **Docker 20.10+** and **Docker Compose 2.0+** (for containerized deployment)
- **OpenSSL 1.1.1+** (for TLS certificate generation)

### Network Requirements

- **HTTP Port**: 8080 (configurable)
- **gRPC Port**: 9090 (configurable)
- **Raft Port**: 7000 (configurable, cluster mode only)

**Firewall Rules:**
```bash
# Allow HTTP API
sudo ufw allow 8080/tcp

# Allow gRPC (if enabled)
sudo ufw allow 9090/tcp

# Allow Raft inter-node communication (cluster mode)
sudo ufw allow 7000/tcp
```

---

## Single-Node Deployment

### Binary Installation

#### Option 1: Build from Source

```bash
# Clone repository
git clone https://github.com/RashikAnsar/raftkv.git
cd raftkv

# Build
make build

# Verify build
./bin/kvstore --version

# Install to /usr/local/bin (optional)
sudo cp ./bin/kvstore /usr/local/bin/
sudo chmod +x /usr/local/bin/kvstore
```

#### Option 2: Download Pre-built Binary

```bash
# Download latest release
curl -LO https://github.com/RashikAnsar/raftkv/releases/download/v1.0.0/kvstore-linux-amd64
chmod +x kvstore-linux-amd64
sudo mv kvstore-linux-amd64 /usr/local/bin/kvstore

# Verify
kvstore --version
```

### Configuration

Create a configuration file:

```bash
sudo mkdir -p /etc/raftkv
sudo nano /etc/raftkv/config.yaml
```

**Basic Configuration:**

```yaml
server:
  http_addr: ":8080"
  grpc_addr: ":9090"
  enable_grpc: false

storage:
  data_dir: "/var/lib/raftkv"
  sync_on_write: true
  snapshot_every: 10000

wal:
  batch_enabled: true
  batch_size: 100
  batch_wait_time: 10ms
  compaction_enabled: true
  compaction_margin: 100

cache:
  enabled: true
  max_size: 10000
  ttl: 0

auth:
  enabled: true
  jwt_secret: "${RAFTKV_JWT_SECRET}"
  token_expiry: 1h

tls:
  enabled: false

observability:
  log_level: "info"
  metrics_enabled: true

performance:
  read_timeout: 10s
  write_timeout: 10s
  max_request_size: 1048576
  enable_rate_limit: false
  rate_limit: 1000
```

### Create Data Directory

```bash
sudo mkdir -p /var/lib/raftkv
sudo chown $USER:$USER /var/lib/raftkv
```

### Set Environment Variables

```bash
# Generate a secure JWT secret
JWT_SECRET=$(openssl rand -base64 32)

# Add to environment
echo "export RAFTKV_JWT_SECRET='$JWT_SECRET'" >> ~/.bashrc
source ~/.bashrc

# Or add to systemd service (recommended for production)
```

### Start the Server

#### Manual Start (Development)

```bash
kvstore --config /etc/raftkv/config.yaml
```

#### Systemd Service (Production)

Create a systemd service file:

```bash
sudo nano /etc/systemd/system/raftkv.service
```

**Service File:**

```ini
[Unit]
Description=RaftKV Distributed Key-Value Store
Documentation=https://github.com/RashikAnsar/raftkv
After=network.target

[Service]
Type=simple
User=raftkv
Group=raftkv
WorkingDirectory=/var/lib/raftkv

# Environment
Environment="RAFTKV_JWT_SECRET=<your-secret-here>"

# Start command
ExecStart=/usr/local/bin/kvstore --config /etc/raftkv/config.yaml

# Restart policy
Restart=on-failure
RestartSec=5s

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=raftkv

[Install]
WantedBy=multi-user.target
```

**Create User:**

```bash
sudo useradd -r -s /bin/false raftkv
sudo chown -R raftkv:raftkv /var/lib/raftkv
```

**Enable and Start Service:**

```bash
# Reload systemd
sudo systemctl daemon-reload

# Enable service (start on boot)
sudo systemctl enable raftkv

# Start service
sudo systemctl start raftkv

# Check status
sudo systemctl status raftkv

# View logs
sudo journalctl -u raftkv -f
```

### Verification

```bash
# Check health
curl http://localhost:8080/health

# Response: {"status":"healthy"}

# Login (default: admin/admin)
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' \
  | jq -r '.token')

# Test write
curl -X PUT http://localhost:8080/keys/test \
  -H "Authorization: Bearer $TOKEN" \
  -d "Hello, RaftKV!"

# Test read
curl http://localhost:8080/keys/test \
  -H "Authorization: Bearer $TOKEN"

# Response: Hello, RaftKV!
```

### Post-Deployment

**Change default admin password:**

```bash
curl -X PUT http://localhost:8080/auth/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "old_password": "admin",
    "new_password": "YourSecurePassword123!"
  }'
```

---

## 3-Node Cluster Deployment

### Architecture

```
┌─────────────┐
│   HAProxy   │  Load Balancer (optional)
│   :8080     │
└──────┬──────┘
       │
   ┌───┴───┬───────┬───────┐
   │       │       │       │
┌──▼──┐ ┌──▼──┐ ┌──▼──┐    │
│Node1│ │Node2│ │Node3│    │
│:8081│ │:8082│ │:8083│    │
│:7000│ │:7001│ │:7002│    │ (Raft ports)
└─────┘ └─────┘ └─────┘    │
   └────────┴───────┴──────┘
      Raft Consensus
```

### Prerequisites

- **3 servers** (or VMs) with network connectivity
- **Unique hostnames**: `node1`, `node2`, `node3`
- **DNS/hosts** configured for inter-node communication

**Update /etc/hosts on all nodes:**

```bash
# /etc/hosts
192.168.1.10  node1
192.168.1.11  node2
192.168.1.12  node3
```

### Node 1 Configuration (Bootstrap Node)

**File:** `/etc/raftkv/config.yaml`

```yaml
server:
  http_addr: ":8080"

storage:
  data_dir: "/var/lib/raftkv"
  sync_on_write: true
  snapshot_every: 10000

wal:
  batch_enabled: true
  batch_size: 100
  batch_wait_time: 10ms
  compaction_enabled: true

cache:
  enabled: true
  max_size: 10000

raft:
  enabled: true
  node_id: "node1"
  raft_addr: "node1:7000"
  raft_dir: "/var/lib/raftkv/raft"
  bootstrap: true  # Bootstrap the cluster

auth:
  enabled: true
  jwt_secret: "${RAFTKV_JWT_SECRET}"
  token_expiry: 1h

observability:
  log_level: "info"
  metrics_enabled: true
```

**Start Node 1:**

```bash
# Set JWT secret (use same secret on all nodes)
export RAFTKV_JWT_SECRET="shared-secret-key-here"

# Start via systemd
sudo systemctl start raftkv

# Verify it's running as leader
curl http://node1:8080/cluster/leader

# Response: {"leader":"node1:8080","state":"Leader"}
```

### Node 2 Configuration (Join Node)

**File:** `/etc/raftkv/config.yaml`

```yaml
server:
  http_addr: ":8080"

storage:
  data_dir: "/var/lib/raftkv"
  sync_on_write: true
  snapshot_every: 10000

wal:
  batch_enabled: true
  batch_size: 100
  batch_wait_time: 10ms
  compaction_enabled: true

cache:
  enabled: true
  max_size: 10000

raft:
  enabled: true
  node_id: "node2"
  raft_addr: "node2:7000"
  raft_dir: "/var/lib/raftkv/raft"
  bootstrap: false
  join_addr: "http://node1:8080/cluster/join"  # Join node1

auth:
  enabled: true
  jwt_secret: "${RAFTKV_JWT_SECRET}"
  token_expiry: 1h

observability:
  log_level: "info"
  metrics_enabled: true
```

**Start Node 2:**

```bash
export RAFTKV_JWT_SECRET="shared-secret-key-here"
sudo systemctl start raftkv

# Verify join
curl http://node2:8080/cluster/nodes

# Should show 2 nodes
```

### Node 3 Configuration (Join Node)

**File:** `/etc/raftkv/config.yaml`

```yaml
server:
  http_addr: ":8080"

storage:
  data_dir: "/var/lib/raftkv"
  sync_on_write: true
  snapshot_every: 10000

wal:
  batch_enabled: true
  batch_size: 100
  batch_wait_time: 10ms
  compaction_enabled: true

cache:
  enabled: true
  max_size: 10000

raft:
  enabled: true
  node_id: "node3"
  raft_addr: "node3:7000"
  raft_dir: "/var/lib/raftkv/raft"
  bootstrap: false
  join_addr: "http://node1:8080/cluster/join"

auth:
  enabled: true
  jwt_secret: "${RAFTKV_JWT_SECRET}"
  token_expiry: 1h

observability:
  log_level: "info"
  metrics_enabled: true
```

**Start Node 3:**

```bash
export RAFTKV_JWT_SECRET="shared-secret-key-here"
sudo systemctl start raftkv

# Verify cluster
curl http://node3:8080/cluster/nodes

# Should show 3 nodes
```

### Cluster Verification

```bash
# Check cluster status (from any node)
curl http://node1:8080/cluster/nodes | jq

# Expected response:
{
  "nodes": [
    {"id": "node1", "address": "node1:7000", "suffrage": "Voter"},
    {"id": "node2", "address": "node2:7000", "suffrage": "Voter"},
    {"id": "node3", "address": "node3:7000", "suffrage": "Voter"}
  ],
  "count": 3
}

# Check leader
curl http://node1:8080/cluster/leader

# Test write to leader
curl -X PUT http://node1:8080/keys/test \
  -H "Authorization: Bearer $TOKEN" \
  -d "Cluster test"

# Read from follower (may be stale)
curl http://node2:8080/keys/test \
  -H "Authorization: Bearer $TOKEN"

# Response: Cluster test
```

### Load Balancer Setup (HAProxy)

**Install HAProxy:**

```bash
sudo apt-get install haproxy
```

**Configuration:** `/etc/haproxy/haproxy.cfg`

```haproxy
global
    log /dev/log local0
    chroot /var/lib/haproxy
    user haproxy
    group haproxy
    daemon

defaults
    log     global
    mode    http
    option  httplog
    option  dontlognull
    timeout connect 5000ms
    timeout client  50000ms
    timeout server  50000ms

frontend raftkv_http
    bind *:8080
    mode http
    default_backend raftkv_nodes

backend raftkv_nodes
    mode http
    balance roundrobin
    option httpchk GET /health

    server node1 node1:8080 check
    server node2 node2:8080 check
    server node3 node3:8080 check

frontend raftkv_stats
    bind *:8404
    stats enable
    stats uri /stats
    stats refresh 10s
```

**Start HAProxy:**

```bash
sudo systemctl enable haproxy
sudo systemctl start haproxy

# Verify
curl http://localhost:8080/health
curl http://localhost:8404/stats
```

---

## Docker Deployment

### Single-Node Docker

**Dockerfile:**

```dockerfile
FROM golang:1.21 AS builder

WORKDIR /app
COPY . .

RUN make build

FROM alpine:latest

RUN apk --no-cache add ca-certificates

COPY --from=builder /app/bin/kvstore /usr/local/bin/

EXPOSE 8080 9090

CMD ["kvstore"]
```

**Build and Run:**

```bash
# Build image
docker build -t raftkv:latest .

# Run container
docker run -d \
  --name raftkv \
  -p 8080:8080 \
  -v /data/raftkv:/data \
  -e RAFTKV_JWT_SECRET="your-secret" \
  raftkv:latest \
  --data-dir=/data \
  --auth --auth-jwt-secret="${RAFTKV_JWT_SECRET}"

# Verify
curl http://localhost:8080/health
```

### 3-Node Docker Compose

**docker-compose.yml:**

```yaml
version: '3.8'

services:
  node1:
    image: raftkv:latest
    container_name: raftkv-node1
    hostname: node1
    networks:
      - raftkv-network
    ports:
      - "8081:8080"
      - "7000:7000"
    volumes:
      - node1-data:/var/lib/raftkv
      - ./config/node1.yaml:/etc/raftkv/config.yaml:ro
    environment:
      - RAFTKV_JWT_SECRET=${RAFTKV_JWT_SECRET}
    restart: unless-stopped

  node2:
    image: raftkv:latest
    container_name: raftkv-node2
    hostname: node2
    networks:
      - raftkv-network
    ports:
      - "8082:8080"
      - "7001:7000"
    volumes:
      - node2-data:/var/lib/raftkv
      - ./config/node2.yaml:/etc/raftkv/config.yaml:ro
    environment:
      - RAFTKV_JWT_SECRET=${RAFTKV_JWT_SECRET}
    depends_on:
      - node1
    restart: unless-stopped

  node3:
    image: raftkv:latest
    container_name: raftkv-node3
    hostname: node3
    networks:
      - raftkv-network
    ports:
      - "8083:8080"
      - "7002:7000"
    volumes:
      - node3-data:/var/lib/raftkv
      - ./config/node3.yaml:/etc/raftkv/config.yaml:ro
    environment:
      - RAFTKV_JWT_SECRET=${RAFTKV_JWT_SECRET}
    depends_on:
      - node1
    restart: unless-stopped

  haproxy:
    image: haproxy:2.9
    container_name: raftkv-haproxy
    networks:
      - raftkv-network
    ports:
      - "8080:8080"
      - "8404:8404"
    volumes:
      - ./haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro
    depends_on:
      - node1
      - node2
      - node3
    restart: unless-stopped

networks:
  raftkv-network:
    driver: bridge

volumes:
  node1-data:
  node2-data:
  node3-data:
```

**Start Cluster:**

```bash
# Set environment variable
export RAFTKV_JWT_SECRET=$(openssl rand -base64 32)

# Start cluster
docker-compose up -d

# Check logs
docker-compose logs -f

# Verify cluster
curl http://localhost:8080/cluster/nodes

# Stop cluster
docker-compose down

# Stop and remove data
docker-compose down -v
```

---

## TLS/mTLS Setup

### Generate Certificates

**Create Certificate Authority (CA):**

```bash
# Create certs directory
mkdir -p certs
cd certs

# Generate CA private key
openssl genrsa -out ca-key.pem 4096

# Generate CA certificate
openssl req -new -x509 -days 3650 -key ca-key.pem -out ca-cert.pem \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=RaftKV CA"
```

**Generate Server Certificates:**

```bash
# Generate server private key
openssl genrsa -out server-key.pem 4096

# Generate server CSR
openssl req -new -key server-key.pem -out server.csr \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=localhost"

# Create extensions file for SAN
cat > server-ext.cnf <<EOF
subjectAltName = DNS:localhost,DNS:node1,DNS:node2,DNS:node3,IP:127.0.0.1,IP:192.168.1.10,IP:192.168.1.11,IP:192.168.1.12
EOF

# Sign server certificate
openssl x509 -req -days 365 -in server.csr \
  -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
  -out server-cert.pem -extfile server-ext.cnf
```

**Generate Client Certificates (for mTLS):**

```bash
# Generate client private key
openssl genrsa -out client-key.pem 4096

# Generate client CSR
openssl req -new -key client-key.pem -out client.csr \
  -subj "/C=US/ST=State/L=City/O=Organization/CN=raftkv-client"

# Sign client certificate
openssl x509 -req -days 365 -in client.csr \
  -CA ca-cert.pem -CAkey ca-key.pem -CAcreateserial \
  -out client-cert.pem
```

**Verify Certificates:**

```bash
# Verify server certificate
openssl verify -CAfile ca-cert.pem server-cert.pem

# Verify client certificate
openssl verify -CAfile ca-cert.pem client-cert.pem

# View certificate details
openssl x509 -in server-cert.pem -text -noout
```

### Configure TLS

**Configuration:** `/etc/raftkv/config.yaml`

```yaml
tls:
  enabled: true
  cert_file: "/etc/raftkv/certs/server-cert.pem"
  key_file: "/etc/raftkv/certs/server-key.pem"
  ca_file: "/etc/raftkv/certs/ca-cert.pem"
  enable_mtls: false  # Set to true for mutual TLS
```

**Copy Certificates:**

```bash
sudo mkdir -p /etc/raftkv/certs
sudo cp certs/* /etc/raftkv/certs/
sudo chown -R raftkv:raftkv /etc/raftkv/certs
sudo chmod 600 /etc/raftkv/certs/*-key.pem
sudo chmod 644 /etc/raftkv/certs/*-cert.pem
```

**Restart Service:**

```bash
sudo systemctl restart raftkv
```

### Test TLS Connection

**HTTP with TLS:**

```bash
# Test with curl (server verification)
curl --cacert /etc/raftkv/certs/ca-cert.pem \
  https://localhost:8080/health

# Test with mTLS
curl --cacert /etc/raftkv/certs/ca-cert.pem \
  --cert /etc/raftkv/certs/client-cert.pem \
  --key /etc/raftkv/certs/client-key.pem \
  https://localhost:8080/health
```

---

## Authentication Setup

### Enable Authentication

**Configuration:**

```yaml
auth:
  enabled: true
  jwt_secret: "${RAFTKV_JWT_SECRET}"
  token_expiry: 1h
```

### Change Default Admin Password

```bash
# Login as admin
TOKEN=$(curl -s -k -X POST https://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' \
  | jq -r '.token')

# Change password
curl -k -X PUT https://localhost:8080/auth/password \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "old_password": "admin",
    "new_password": "NewSecurePassword123!"
  }'
```

### Create Users

```bash
# Create read-only user
curl -k -X POST https://localhost:8080/auth/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "readonly",
    "password": "ReadOnlyPass123!",
    "role": "read"
  }'

# Create write user
curl -k -X POST https://localhost:8080/auth/users \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "writer",
    "password": "WritePass123!",
    "role": "write"
  }'
```

### Create API Keys

```bash
# Create production API key
curl -k -X POST https://localhost:8080/auth/api-keys \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production API Key",
    "role": "write",
    "expires_in": "8760h"
  }' | jq -r '.key'

# Save the returned key securely!
```

---

## Monitoring Setup

See [MONITORING.md](MONITORING.md) for complete monitoring setup with Prometheus and Grafana.

**Quick Setup:**

```bash
# Start Prometheus
docker run -d \
  --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/deployments/monitoring/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus:latest

# Start Grafana
docker run -d \
  --name grafana \
  -p 3000:3000 \
  grafana/grafana:latest

# Import dashboards from deployments/monitoring/grafana/dashboards/
```

---

## Production Checklist

### Security

- [ ] Change default admin password
- [ ] Enable TLS (`tls.enabled: true`)
- [ ] Enable authentication (`auth.enabled: true`)
- [ ] Use strong JWT secret (32+ characters)
- [ ] Rotate API keys regularly
- [ ] Configure firewall rules
- [ ] Enable mTLS for inter-node communication (cluster mode)
- [ ] Restrict admin endpoints to internal network

### Performance

- [ ] Enable batch writes (`wal.batch_enabled: true`)
- [ ] Enable LRU cache (`cache.enabled: true`)
- [ ] Enable WAL compaction (`wal.compaction_enabled: true`)
- [ ] Configure snapshot interval (`storage.snapshot_every: 10000`)
- [ ] Tune batch size based on workload
- [ ] Monitor disk I/O and adjust accordingly
- [ ] Set appropriate resource limits (systemd)

### Reliability

- [ ] Enable systemd service
- [ ] Configure automatic restarts (`Restart=on-failure`)
- [ ] Set up log rotation (journald or logrotate)
- [ ] Configure backup strategy
- [ ] Test failover scenarios (cluster mode)
- [ ] Document recovery procedures
- [ ] Set up monitoring and alerting

### Monitoring

- [ ] Configure Prometheus scraping
- [ ] Import Grafana dashboards
- [ ] Set up critical alerts (no leader, high error rate)
- [ ] Set up warning alerts (high latency, low cache hit rate)
- [ ] Configure alert notifications (email, Slack, PagerDuty)
- [ ] Monitor disk usage (WAL segments)
- [ ] Monitor Raft lag (cluster mode)

### Documentation

- [ ] Document deployment architecture
- [ ] Document node IP addresses and roles
- [ ] Document authentication credentials (securely)
- [ ] Document backup/restore procedures
- [ ] Document runbook for common operations
- [ ] Document escalation procedures

---

## Troubleshooting

### Service Won't Start

**Check logs:**
```bash
sudo journalctl -u raftkv -n 100 --no-pager
```

**Common issues:**
- Port already in use: Check with `sudo netstat -tulpn | grep 8080`
- Permission denied: Check file ownership and permissions
- Config file errors: Validate YAML syntax
- Missing JWT secret: Ensure `RAFTKV_JWT_SECRET` is set

### Cluster Formation Issues

**Node won't join cluster:**

```bash
# Check join_addr is correct
curl http://node1:8080/cluster/nodes

# Check network connectivity
ping node1
telnet node1 7000

# Check firewall rules
sudo ufw status

# Check Raft logs
sudo journalctl -u raftkv | grep -i raft
```

**No leader elected:**

```bash
# Check cluster status on all nodes
for node in node1 node2 node3; do
  echo "=== $node ==="
  curl http://$node:8080/cluster/leader
done

# Verify Raft addresses
curl http://node1:8080/cluster/nodes

# Check for split-brain (network partition)
# Ensure majority (2/3) of nodes can communicate
```

### High Latency

**Check metrics:**
```bash
curl http://localhost:8080/metrics | grep duration
```

**Common causes:**
- Disk I/O bottleneck: Check `iostat -x 1`
- Batching disabled: Enable `wal.batch_enabled`
- Low cache hit rate: Increase `cache.max_size`
- Network latency: Check inter-node latency
- Resource constraints: Check CPU/memory usage

### Disk Space Issues

**Check WAL size:**
```bash
du -sh /var/lib/raftkv/*.wal

# Check total disk usage
df -h /var/lib/raftkv
```

**Solutions:**
- Trigger manual snapshot: `curl -X POST http://localhost:8080/admin/snapshot`
- Enable WAL compaction: `wal.compaction_enabled: true`
- Reduce snapshot interval: `storage.snapshot_every: 5000`
- Add disk space

### Certificate Issues

**Verify certificate chain:**
```bash
openssl verify -CAfile ca-cert.pem server-cert.pem
```

**Check certificate expiry:**
```bash
openssl x509 -in server-cert.pem -noout -dates
```

**Test TLS connection:**
```bash
openssl s_client -connect localhost:8080 -CAfile ca-cert.pem
```

---

## Next Steps

- [OPERATIONS.md](OPERATIONS.md) - Operations runbook (backup, restore, monitoring)
- [API_REFERENCE.md](API_REFERENCE.md) - Complete API documentation
- [ARCHITECTURE.md](ARCHITECTURE.md) - System architecture details
