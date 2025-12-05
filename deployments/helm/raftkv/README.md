# RaftKV Helm Chart

A Helm chart for deploying RaftKV, a production-grade distributed key-value store with Raft consensus, on Kubernetes.

## Overview

This chart deploys a RaftKV cluster as a StatefulSet with:
- **Raft Consensus**: Distributed consensus for data consistency and fault tolerance
- **High Availability**: Multiple replicas with automatic leader election
- **Persistent Storage**: Data persistence using PersistentVolumeClaims
- **Service Discovery**: Headless service for Raft peer-to-peer communication
- **Load Balancing**: LoadBalancer service for client access
- **Monitoring**: Prometheus metrics and optional ServiceMonitor
- **Security**: RBAC, Pod Security Context, optional TLS/mTLS
- **Authentication**: JWT-based authentication

## Prerequisites

- Kubernetes 1.24+
- Helm 3.8+
- PV provisioner support in the underlying infrastructure
- (Optional) Prometheus Operator for ServiceMonitor

## Quick Start

### 1. Add Helm Repository (Future)

```bash
# Once published to a Helm repository
helm repo add raftkv https://your-helm-repo.com
helm repo update
```

### 2. Install from Local Chart

```bash
# From the helm directory
cd helm
helm install raftkv ./raftkv -n raftkv --create-namespace
```

### 3. Install with Custom Values

```bash
# Development environment
helm install raftkv ./raftkv -n raftkv --create-namespace \
  -f ./raftkv/values-development.yaml

# Production environment
helm install raftkv ./raftkv -n raftkv --create-namespace \
  -f ./raftkv/values-production.yaml
```

### 4. Verify Installation

```bash
# Check pods
kubectl get pods -n raftkv

# Check services
kubectl get svc -n raftkv

# Check cluster status
kubectl exec -n raftkv raftkv-0 -- wget -q -O- http://localhost:8080/cluster/nodes
```

## Configuration

### Key Configuration Parameters

#### Replica Count

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of RaftKV replicas (minimum 3 for Raft quorum) | `3` |

**Recommendation**: Use odd numbers (3, 5, 7) for better quorum handling.

#### Image

| Parameter | Description | Default |
|-----------|-------------|---------|
| `image.repository` | RaftKV Docker image repository | `raftkv` |
| `image.pullPolicy` | Image pull policy | `IfNotPresent` |
| `image.tag` | Image tag (overrides chart appVersion) | `latest` |
| `imagePullSecrets` | Image pull secrets for private registries | `[]` |

#### Service

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Service type (LoadBalancer, ClusterIP, NodePort) | `LoadBalancer` |
| `service.httpPort` | HTTP API port | `8080` |
| `service.grpcPort` | gRPC API port | `9090` |
| `service.headless.raftPort` | Raft communication port | `7000` |
| `service.headless.metricsPort` | Prometheus metrics port | `2112` |

#### Resources

| Parameter | Description | Default |
|-----------|-------------|---------|
| `resources.requests.cpu` | CPU request | `1000m` |
| `resources.requests.memory` | Memory request | `2Gi` |
| `resources.limits.cpu` | CPU limit | `2000m` |
| `resources.limits.memory` | Memory limit | `4Gi` |

#### Persistence

| Parameter | Description | Default |
|-----------|-------------|---------|
| `persistence.enabled` | Enable persistent storage | `true` |
| `persistence.storageClass` | Storage class name | `""` (default) |
| `persistence.accessMode` | PVC access mode | `ReadWriteOnce` |
| `persistence.size` | Storage size per pod | `10Gi` |

#### RaftKV Application

| Parameter | Description | Default |
|-----------|-------------|---------|
| `raftkv.raft.enabled` | Enable Raft consensus | `true` |
| `raftkv.auth.enabled` | Enable JWT authentication | `true` |
| `raftkv.auth.jwtSecret` | JWT secret (auto-generated if empty) | `""` |
| `raftkv.auth.adminPassword` | Default admin password | `"admin"` |
| `raftkv.storage.wal.enabled` | Enable Write-Ahead Log | `true` |
| `raftkv.storage.cache.enabled` | Enable LRU cache | `true` |
| `raftkv.tls.enabled` | Enable TLS | `false` |

#### Monitoring

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceMonitor.enabled` | Create Prometheus ServiceMonitor | `false` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `raftkv.observability.metrics.enabled` | Enable metrics endpoint | `true` |

### Complete Values

See [values.yaml](values.yaml) for the complete list of configurable parameters.

## Installation Examples

### Local Development (Minikube)

```bash
# Start Minikube
minikube start --cpus=4 --memory=8192

# Load Docker image (if built locally)
minikube image load raftkv:latest

# Install with development values
helm install raftkv ./raftkv -n raftkv --create-namespace \
  -f ./raftkv/values-development.yaml

# Port-forward to access
kubectl port-forward -n raftkv svc/raftkv 8080:8080

# Test the API
curl http://localhost:8080/health
curl http://localhost:8080/cluster/nodes
```

### Production Deployment

```bash
# Create namespace
kubectl create namespace raftkv

# Create image pull secret (if using private registry)
kubectl create secret docker-registry raftkv-registry-secret \
  --docker-server=your-registry.io \
  --docker-username=your-username \
  --docker-password=your-password \
  -n raftkv

# Create TLS secret (if enabling TLS)
kubectl create secret generic raftkv-tls \
  --from-file=ca-cert.pem=path/to/ca-cert.pem \
  --from-file=server-cert.pem=path/to/server-cert.pem \
  --from-file=server-key.pem=path/to/server-key.pem \
  -n raftkv

# Install with production values
helm install raftkv ./raftkv -n raftkv \
  -f ./raftkv/values-production.yaml \
  --set image.repository=your-registry.io/raftkv \
  --set image.tag=v1.0.0 \
  --set raftkv.auth.jwtSecret="$(openssl rand -base64 32)"

# Verify deployment
kubectl get pods -n raftkv -w
```

### Custom Configuration

Create a custom values file:

```yaml
# custom-values.yaml
replicaCount: 5

image:
  repository: myregistry.io/raftkv
  tag: v1.0.0

service:
  type: LoadBalancer
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"

persistence:
  storageClass: "fast-ssd"
  size: 50Gi

resources:
  requests:
    cpu: 2000m
    memory: 4Gi
  limits:
    cpu: 4000m
    memory: 8Gi

raftkv:
  storage:
    wal:
      syncWrites: true
    cache:
      maxSize: 50000

serviceMonitor:
  enabled: true
  additionalLabels:
    release: prometheus
```

Install with custom values:

```bash
helm install raftkv ./raftkv -n raftkv --create-namespace -f custom-values.yaml
```

## Operations

### Scaling

```bash
# Scale up to 5 replicas
helm upgrade raftkv ./raftkv -n raftkv --reuse-values --set replicaCount=5

# Or edit values and upgrade
helm upgrade raftkv ./raftkv -n raftkv -f custom-values.yaml
```

**Note**: New nodes will automatically join the Raft cluster.

### Upgrading

```bash
# Upgrade to new version
helm upgrade raftkv ./raftkv -n raftkv \
  --set image.tag=v1.1.0 \
  --reuse-values

# Check rollout status
kubectl rollout status statefulset/raftkv -n raftkv
```

### Backup and Restore

```bash
# Trigger manual snapshot
kubectl exec -n raftkv raftkv-0 -- wget -q -O- --post-data='' http://localhost:8080/admin/snapshot

# Copy snapshot from pod
kubectl cp raftkv/raftkv-0:/var/lib/raftkv/snapshots ./backups/

# Restore (requires scaling down and PVC recreation)
# See detailed restore procedure in main documentation
```

### Rollback

```bash
# Rollback to previous version
helm rollback raftkv -n raftkv

# Rollback to specific revision
helm rollback raftkv 1 -n raftkv
```

### Uninstall

```bash
# Uninstall release (keeps PVCs)
helm uninstall raftkv -n raftkv

# Delete PVCs
kubectl delete pvc -n raftkv -l app.kubernetes.io/name=raftkv

# Delete namespace
kubectl delete namespace raftkv
```

## Monitoring

### Prometheus Integration

If you have Prometheus Operator installed:

```yaml
# values.yaml
serviceMonitor:
  enabled: true
  interval: 30s
  additionalLabels:
    release: prometheus  # Match your Prometheus release label
```

### Grafana Dashboard

Import the RaftKV Grafana dashboard from `deployments/monitoring/grafana/`.

### Viewing Metrics

```bash
# Port-forward metrics endpoint
kubectl port-forward -n raftkv raftkv-0 2112:2112

# View metrics
curl http://localhost:2112/metrics | grep raftkv
```

## Troubleshooting

### Pods Not Starting

```bash
# Check pod status
kubectl get pods -n raftkv

# Describe pod for events
kubectl describe pod -n raftkv raftkv-0

# Check logs
kubectl logs -n raftkv raftkv-0

# Common issues:
# - ImagePullBackOff: Check image name and pull secrets
# - Pending: Check PVC and storage class
# - CrashLoopBackOff: Check logs and resource limits
```

### Cluster Not Forming

```bash
# Check if raftkv-0 bootstrapped
kubectl logs -n raftkv raftkv-0 | grep -i bootstrap

# Check if other pods joined
kubectl logs -n raftkv raftkv-1 | grep -i "join\|cluster"

# Check cluster status from each pod
for i in 0 1 2; do
  echo "=== raftkv-$i ==="
  kubectl exec -n raftkv raftkv-$i -- wget -q -O- http://localhost:8080/cluster/nodes
done
```

### Storage Issues

```bash
# Check PVCs
kubectl get pvc -n raftkv

# Check PVs
kubectl get pv | grep raftkv

# Describe PVC for events
kubectl describe pvc -n raftkv data-raftkv-0
```

### Leader Election Issues

```bash
# Check leader from each pod
for i in 0 1 2; do
  echo "=== raftkv-$i ==="
  kubectl exec -n raftkv raftkv-$i -- wget -q -O- http://localhost:8080/cluster/leader
done

# Should all return the same leader
# If different, restart all pods:
kubectl delete pod -n raftkv raftkv-0 raftkv-1 raftkv-2
```

## Security

### Production Security Checklist

- [ ] Change default admin password
- [ ] Generate strong JWT secret (use external secret management)
- [ ] Enable TLS/mTLS
- [ ] Use private container registry with image pull secrets
- [ ] Configure Pod Security Standards
- [ ] Set up RBAC with least privilege
- [ ] Enable network policies
- [ ] Use non-root user (already configured)
- [ ] Enable audit logging
- [ ] Configure resource quotas

### Using External Secrets

With Sealed Secrets:

```bash
# Create sealed secret for JWT
kubectl create secret generic raftkv-secret \
  --from-literal=jwt-secret="$(openssl rand -base64 32)" \
  --dry-run=client -o yaml | \
  kubeseal -o yaml > raftkv-sealed-secret.yaml

kubectl apply -f raftkv-sealed-secret.yaml -n raftkv
```

With HashiCorp Vault:

```yaml
# values.yaml
extraEnv:
  - name: RAFTKV_AUTH__JWT_SECRET
    valueFrom:
      secretKeyRef:
        name: vault-secret
        key: jwt-secret
```

## FAQ

**Q: What is the minimum number of replicas?**
A: 3 replicas (minimum for Raft quorum). For production, 5 replicas is recommended.

**Q: Can I use this on a single-node cluster like Minikube?**
A: Yes, use `values-development.yaml` which uses preferred (not required) pod anti-affinity.

**Q: How do I change the admin password?**
A: Set `raftkv.auth.adminPassword` in values or use external secret management.

**Q: What storage class should I use?**
A: For production, use fast SSD storage (e.g., `gp3` on AWS, `pd-ssd` on GCP). For development, the default storage class works fine.

**Q: How do I enable TLS?**
A: Set `raftkv.tls.enabled=true` and create a TLS secret with your certificates.

**Q: Can I disable authentication?**
A: Yes, set `raftkv.auth.enabled=false`. Not recommended for production.

**Q: How do I upgrade RaftKV version?**
A: Update `image.tag` and run `helm upgrade`. StatefulSet will perform rolling updates.

## Contributing

Issues and pull requests are welcome at https://github.com/RashikAnsar/raftkv

## License

Apache 2.0

## Links

- **GitHub**: https://github.com/RashikAnsar/raftkv
- **Documentation**: https://github.com/RashikAnsar/raftkv/blob/main/README.md
- **Architecture**: https://github.com/RashikAnsar/raftkv/blob/main/docs/ARCHITECTURE.md
- **Issues**: https://github.com/RashikAnsar/raftkv/issues
