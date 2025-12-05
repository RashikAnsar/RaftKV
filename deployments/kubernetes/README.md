# RaftKV Kubernetes Deployment

This directory contains Kubernetes manifests for deploying RaftKV as a StatefulSet cluster.

## Overview

The deployment creates a 3-node RaftKV cluster with:
- **StatefulSet**: 3 replicas with stable network identities
- **Headless Service**: For Raft peer-to-peer communication
- **LoadBalancer Service**: For client HTTP/gRPC access
- **PersistentVolumeClaims**: 10Gi storage per pod
- **RBAC**: ServiceAccount with minimal permissions
- **ConfigMap**: RaftKV configuration
- **Secret**: JWT secrets and credentials

## Prerequisites

- Kubernetes cluster (v1.24+)
- kubectl configured
- Sufficient resources:
  - 3 nodes (or node anti-affinity disabled)
  - 3 CPU cores (1 per pod)
  - 6 GB RAM (2 GB per pod)
  - 30 GB storage (10 GB per pod)

## Quick Start

### 1. Deploy to Kubernetes

```bash
# Using kubectl
kubectl apply -k .

# Or using kustomize
kustomize build . | kubectl apply -f -
```

### 2. Verify Deployment

```bash
# Check pods
kubectl get pods -n raftkv

# Expected output:
# NAME       READY   STATUS    RESTARTS   AGE
# raftkv-0   1/1     Running   0          2m
# raftkv-1   1/1     Running   0          2m
# raftkv-2   1/1     Running   0          2m

# Check services
kubectl get svc -n raftkv

# Check StatefulSet
kubectl get statefulset -n raftkv

# View logs
kubectl logs -n raftkv raftkv-0 -f
```

### 3. Access RaftKV

```bash
# Port-forward for local access
kubectl port-forward -n raftkv svc/raftkv 8080:8080

# In another terminal, test the API
curl http://localhost:8080/health

# Check cluster status
curl http://localhost:8080/cluster/nodes
```

## Configuration

### Update Image

Edit `kustomization.yaml`:

```yaml
images:
  - name: raftkv
    newName: your-registry/raftkv
    newTag: v1.0.0
```

### Change Storage Class

Edit `statefulset.yaml` volumeClaimTemplates:

```yaml
storageClassName: fast-ssd  # Your storage class
```

### Adjust Resources

Edit `statefulset.yaml` resources:

```yaml
resources:
  requests:
    cpu: 1000m      # 1 CPU
    memory: 2Gi     # 2 GB RAM
  limits:
    cpu: 2000m      # 2 CPU
    memory: 4Gi     # 4 GB RAM
```

### Enable TLS

1. Create TLS certificates:

```bash
# Generate certificates (or use cert-manager)
make tls-certs

# Create secret
kubectl create secret generic raftkv-tls \
  --from-file=ca-cert.pem=certs/ca-cert.pem \
  --from-file=server-cert.pem=certs/server-cert.pem \
  --from-file=server-key.pem=certs/server-key.pem \
  -n raftkv
```

2. Update `configmap.yaml`:

```yaml
tls:
  enabled: true
  cert_file: "/etc/raftkv/tls/server-cert.pem"
  key_file: "/etc/raftkv/tls/server-key.pem"
  ca_file: "/etc/raftkv/tls/ca-cert.pem"
```

3. Mount TLS secret in `statefulset.yaml`:

```yaml
volumeMounts:
  - name: tls
    mountPath: /etc/raftkv/tls
    readOnly: true

volumes:
  - name: tls
    secret:
      secretName: raftkv-tls
```

## Operations

### Scale Cluster

```bash
# Scale to 5 nodes
kubectl scale statefulset raftkv --replicas=5 -n raftkv

# Note: New nodes will automatically join the cluster
```

### Rolling Update

```bash
# Update image version in kustomization.yaml, then:
kubectl apply -k .

# Or manually rollout
kubectl rollout restart statefulset/raftkv -n raftkv

# Check rollout status
kubectl rollout status statefulset/raftkv -n raftkv
```

### Backup Data

```bash
# Trigger snapshot via API
kubectl exec -n raftkv raftkv-0 -- curl -X POST http://localhost:8080/admin/snapshot

# Or copy data from pod
kubectl cp raftkv/raftkv-0:/var/lib/raftkv/snapshots ./backups/
```

### Restore from Backup

```bash
# 1. Scale down to 0
kubectl scale statefulset raftkv --replicas=0 -n raftkv

# 2. Delete PVCs
kubectl delete pvc -n raftkv -l app=raftkv

# 3. Scale back up (will create new PVCs)
kubectl scale statefulset raftkv --replicas=3 -n raftkv

# 4. Copy snapshot to raftkv-0
kubectl cp ./backups/snapshot-xxx.gob raftkv/raftkv-0:/var/lib/raftkv/snapshots/

# 5. Restart raftkv-0
kubectl delete pod -n raftkv raftkv-0
```

## Troubleshooting

### Pods Not Starting

```bash
# Check pod events
kubectl describe pod -n raftkv raftkv-0

# Check logs
kubectl logs -n raftkv raftkv-0

# Common issues:
# - Insufficient resources
# - Storage class not available
# - Image pull errors
```

### Leader Not Elected

```bash
# Check cluster status from each pod
for i in 0 1 2; do
  echo "=== raftkv-$i ==="
  kubectl exec -n raftkv raftkv-$i -- curl -s http://localhost:8080/cluster/nodes
done

# Check Raft logs
kubectl logs -n raftkv raftkv-0 | grep -i "raft\|leader\|election"
```

### Split Brain

If you suspect split brain (multiple leaders):

```bash
# Check leader from each pod
for i in 0 1 2; do
  echo "=== raftkv-$i ==="
  kubectl exec -n raftkv raftkv-$i -- curl -s http://localhost:8080/cluster/leader
done

# Should all return the same leader
# If different, restart all pods:
kubectl delete pod -n raftkv raftkv-0 raftkv-1 raftkv-2
```

### Network Issues

```bash
# Test pod-to-pod communication
kubectl exec -n raftkv raftkv-0 -- ping raftkv-1.raftkv-headless.raftkv.svc.cluster.local

# Test DNS resolution
kubectl exec -n raftkv raftkv-0 -- nslookup raftkv-headless.raftkv.svc.cluster.local

# Check service endpoints
kubectl get endpoints -n raftkv raftkv-headless
```

### Storage Issues

```bash
# Check PVCs
kubectl get pvc -n raftkv

# Check PV
kubectl get pv | grep raftkv

# Describe PVC for events
kubectl describe pvc -n raftkv data-raftkv-0
```

## Monitoring

### Prometheus Integration

If using Prometheus Operator:

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: raftkv
  namespace: raftkv
spec:
  selector:
    matchLabels:
      app: raftkv
  endpoints:
    - port: metrics
      interval: 30s
      path: /metrics
```

### Grafana Dashboard

1. Port-forward Grafana:

```bash
kubectl port-forward -n monitoring svc/grafana 3000:3000
```

2. Import dashboard from `deployments/monitoring/grafana/`

### View Metrics

```bash
# Get metrics from a pod
kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:2112/metrics
```

## Security

### Production Checklist

- [ ] Change default JWT secret in `secret.yaml`
- [ ] Change default admin password
- [ ] Enable TLS/mTLS
- [ ] Use external secrets management (Vault, Sealed Secrets)
- [ ] Configure network policies
- [ ] Enable Pod Security Standards
- [ ] Set up RBAC with least privilege
- [ ] Enable audit logging
- [ ] Configure resource quotas
- [ ] Set up backups

### Network Policy Example

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: raftkv-network-policy
  namespace: raftkv
spec:
  podSelector:
    matchLabels:
      app: raftkv
  policyTypes:
    - Ingress
    - Egress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: raftkv
      ports:
        - protocol: TCP
          port: 7000  # Raft
    - from: []  # Allow from anywhere
      ports:
        - protocol: TCP
          port: 8080  # HTTP
        - protocol: TCP
          port: 9090  # gRPC
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: raftkv
      ports:
        - protocol: TCP
          port: 7000
    - to: []  # Allow DNS
      ports:
        - protocol: UDP
          port: 53
```

## Advanced Configuration

### Custom Storage Configuration

For production, use faster storage:

```yaml
# fast-storage-class.yaml
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: fast-ssd
provisioner: kubernetes.io/aws-ebs  # or gce-pd, azure-disk
parameters:
  type: gp3  # AWS EBS gp3
  iops: "3000"
  throughput: "125"
volumeBindingMode: WaitForFirstConsumer
```

### Horizontal Pod Autoscaling (Not Recommended)

RaftKV uses Raft consensus, so scaling requires careful coordination:

```bash
# DO NOT use HPA for StatefulSet
# Instead, manually scale and let new nodes join:
kubectl scale statefulset raftkv --replicas=5 -n raftkv
```

### Ingress Configuration

```yaml
# ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: raftkv
  namespace: raftkv
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
    cert-manager.io/cluster-issuer: "letsencrypt-prod"
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - raftkv.example.com
      secretName: raftkv-tls
  rules:
    - host: raftkv.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: raftkv
                port:
                  number: 8080
```

## Clean Up

```bash
# Delete all resources
kubectl delete -k .

# Or delete namespace (removes everything)
kubectl delete namespace raftkv

# Delete PVs (if needed)
kubectl get pv | grep raftkv | awk '{print $1}' | xargs kubectl delete pv
```

## References

- [RaftKV Documentation](../../docs/ARCHITECTURE.md)
- [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/)
- [Kubernetes Storage](https://kubernetes.io/docs/concepts/storage/)
- [Helm Chart](../../deployments/helm/raftkv/README.md)

## Support

For issues or questions:
- GitHub Issues: https://github.com/RashikAnsar/raftkv/issues
- Documentation: [../../docs/](../../docs/)
