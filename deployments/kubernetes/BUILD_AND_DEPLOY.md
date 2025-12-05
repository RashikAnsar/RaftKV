# Building and Deploying RaftKV to Kubernetes

This guide walks you through building the Docker image and deploying RaftKV to Kubernetes.

## Prerequisites

- Docker installed
- kubectl configured
- Minikube or Kubernetes cluster
- Go 1.25+ (for local builds)

---

## Step 1: Build the Docker Image

### Option A: Using Make (Recommended)

```bash
# From the project root directory
make docker-build
```

### Option B: Direct Docker Build

```bash
# From the project root directory
docker build -t raftkv:latest -f deployments/docker/Dockerfile .
```

### What Happens During Build

The Dockerfile uses a **multi-stage build**:

1. **Stage 1 (Builder):**
   - Uses `golang:1.23-alpine`
   - Downloads Go dependencies
   - Builds both `kvstore` (server) and `kvcli` (CLI)

2. **Stage 2 (Runtime):**
   - Uses minimal `alpine:latest`
   - Copies only the binaries
   - Runs as non-root user (`raftkv:raftkv`)
   - Exposes ports: 8080 (HTTP), 9090 (gRPC), 7000 (Raft)

### Verify the Build

```bash
# Check the image
docker images | grep raftkv

# Expected output:
# raftkv       latest    <image-id>    <time>    <size>MB

# Test the image locally
docker run --rm raftkv:latest --version
```

---

## Step 2: Deploy to Minikube (Local Testing)

### 2.1 Start Minikube

```bash
# Start Minikube with sufficient resources
minikube start --cpus=4 --memory=8192

# Verify
minikube status
```

### 2.2 Load Image into Minikube

Minikube has its own Docker daemon, so you need to load the image:

```bash
# Load the local image into Minikube
minikube image load raftkv:latest

# Verify the image is in Minikube
minikube image ls | grep raftkv
```

**Alternative:** Build directly in Minikube's Docker daemon:

```bash
# Point your shell to Minikube's Docker daemon
eval $(minikube docker-env)

# Now build (image will be available in Minikube)
make docker-build

# When done, switch back to local Docker
eval $(minikube docker-env -u)
```

### 2.3 Deploy RaftKV

```bash
# Navigate to Kubernetes manifests
cd deployments/kubernetes

# Deploy using Kustomize
kubectl apply -k .

# Or deploy individual files
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f configmap.yaml
kubectl apply -f secret.yaml
kubectl apply -f service-headless.yaml
kubectl apply -f service-lb.yaml
kubectl apply -f statefulset.yaml
```

### 2.4 Verify Deployment

```bash
# Run the validation script
./validate.sh

# Or manually check
kubectl get pods -n raftkv -w

# Wait for all pods to be Running
# Expected output:
# NAME       READY   STATUS    RESTARTS   AGE
# raftkv-0   1/1     Running   0          2m
# raftkv-1   1/1     Running   0          2m
# raftkv-2   1/1     Running   0          2m
```

### 2.5 Access RaftKV

```bash
# Port-forward the service
kubectl port-forward -n raftkv svc/raftkv 8080:8080

# In another terminal, test the API
curl http://localhost:8080/health

# Check cluster status
curl http://localhost:8080/cluster/nodes

# Expected response (formatted):
# {
#   "nodes": [
#     {"id": "raftkv-0", "role": "leader", ...},
#     {"id": "raftkv-1", "role": "follower", ...},
#     {"id": "raftkv-2", "role": "follower", ...}
#   ]
# }
```

---

## Step 3: Deploy to Production Kubernetes

### 3.1 Push Image to Registry

For production, push the image to a container registry:

#### Docker Hub

```bash
# Tag the image
docker tag raftkv:latest your-username/raftkv:v1.0.0
docker tag raftkv:latest your-username/raftkv:latest

# Login to Docker Hub
docker login

# Push
docker push your-username/raftkv:v1.0.0
docker push your-username/raftkv:latest
```

#### AWS ECR

```bash
# Login to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <account-id>.dkr.ecr.us-east-1.amazonaws.com

# Create repository
aws ecr create-repository --repository-name raftkv --region us-east-1

# Tag
docker tag raftkv:latest <account-id>.dkr.ecr.us-east-1.amazonaws.com/raftkv:v1.0.0

# Push
docker push <account-id>.dkr.ecr.us-east-1.amazonaws.com/raftkv:v1.0.0
```

#### Google Container Registry (GCR)

```bash
# Configure Docker for GCR
gcloud auth configure-docker

# Tag
docker tag raftkv:latest gcr.io/<project-id>/raftkv:v1.0.0

# Push
docker push gcr.io/<project-id>/raftkv:v1.0.0
```

### 3.2 Update Kubernetes Manifests

Edit `kustomization.yaml`:

```yaml
images:
  - name: raftkv
    newName: your-username/raftkv  # or ECR/GCR URL
    newTag: v1.0.0
```

### 3.3 Update Secrets (IMPORTANT!)

**Before deploying to production:**

```bash
# Generate a strong JWT secret (256-bit)
JWT_SECRET=$(openssl rand -base64 32)

# Update the secret
kubectl create secret generic raftkv-secret \
  --from-literal=jwt-secret="$JWT_SECRET" \
  --from-literal=admin-password="your-secure-password" \
  -n raftkv \
  --dry-run=client -o yaml | kubectl apply -f -
```

### 3.4 Configure Storage Class

Edit `statefulset.yaml` volumeClaimTemplates:

```yaml
volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes:
        - ReadWriteOnce
      storageClassName: fast-ssd  # Your production storage class
      resources:
        requests:
          storage: 50Gi  # Increase for production
```

### 3.5 Deploy

```bash
# Deploy to production
kubectl apply -k deployments/kubernetes/

# Monitor rollout
kubectl rollout status statefulset/raftkv -n raftkv
```

---

## Step 4: Post-Deployment Verification

### 4.1 Run Validation

```bash
cd deployments/kubernetes
./validate.sh
```

### 4.2 Check Cluster Health

```bash
# Get cluster status
kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:8080/cluster/nodes

# Check leader
kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:8080/cluster/leader

# View metrics
kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:2112/metrics | grep raftkv
```

### 4.3 Test Basic Operations

```bash
# Port-forward
kubectl port-forward -n raftkv svc/raftkv 8080:8080 &

# Get JWT token
TOKEN=$(curl -s -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' | jq -r '.token')

# Write a key
curl -X PUT http://localhost:8080/keys/test \
  -H "Authorization: Bearer $TOKEN" \
  -d "hello-kubernetes"

# Read the key
curl http://localhost:8080/keys/test \
  -H "Authorization: Bearer $TOKEN"

# Delete the key
curl -X DELETE http://localhost:8080/keys/test \
  -H "Authorization: Bearer $TOKEN"
```

### 4.4 Test Failover

```bash
# Get current leader
LEADER=$(kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:8080/cluster/leader | jq -r '.id')
echo "Current leader: $LEADER"

# Delete the leader pod
kubectl delete pod -n raftkv $LEADER

# Wait and check new leader (should happen within 5 seconds)
sleep 5
NEW_LEADER=$(kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:8080/cluster/leader | jq -r '.id')
echo "New leader: $NEW_LEADER"

# Verify data is still accessible
curl http://localhost:8080/keys/test -H "Authorization: Bearer $TOKEN"
```

---

## Step 5: Monitoring

### 5.1 View Logs

```bash
# Tail logs from all pods
kubectl logs -n raftkv -l app=raftkv -f

# Specific pod
kubectl logs -n raftkv raftkv-0 -f

# Previous instance (if pod restarted)
kubectl logs -n raftkv raftkv-0 --previous
```

### 5.2 Metrics

```bash
# Port-forward Prometheus (if deployed)
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Or get metrics directly from pods
kubectl exec -n raftkv raftkv-0 -- curl -s http://localhost:2112/metrics
```

### 5.3 Resource Usage

```bash
# CPU and Memory usage
kubectl top pods -n raftkv

# Describe pod for detailed info
kubectl describe pod -n raftkv raftkv-0
```

---

## Troubleshooting

### Image Pull Errors

```bash
# Check image pull status
kubectl describe pod -n raftkv raftkv-0 | grep -A 10 Events

# Common issues:
# 1. ImagePullBackOff - Image not found or authentication failed
# 2. ErrImagePull - Network or registry issues
```

**Solutions:**

```bash
# For Minikube: Ensure image is loaded
minikube image load raftkv:latest

# For private registries: Create image pull secret
kubectl create secret docker-registry regcred \
  --docker-server=<registry> \
  --docker-username=<username> \
  --docker-password=<password> \
  -n raftkv

# Update StatefulSet to use the secret
# Add to spec.template.spec:
# imagePullSecrets:
#   - name: regcred
```

### Pods Not Starting

```bash
# Check pod status
kubectl get pods -n raftkv

# Describe problematic pod
kubectl describe pod -n raftkv raftkv-0

# Check logs
kubectl logs -n raftkv raftkv-0

# Common issues:
# 1. Resource constraints - Increase limits or free up cluster resources
# 2. Storage issues - Check PVC status
# 3. Configuration errors - Check configmap and secrets
```

### Cluster Not Forming

```bash
# Check if raftkv-0 bootstrapped
kubectl logs -n raftkv raftkv-0 | grep -i bootstrap

# Check if raftkv-1 and raftkv-2 joined
kubectl logs -n raftkv raftkv-1 | grep -i "join\|cluster"

# Check network connectivity
kubectl exec -n raftkv raftkv-0 -- ping raftkv-1.raftkv-headless.raftkv.svc.cluster.local

# Restart pods if needed
kubectl delete pod -n raftkv raftkv-0 raftkv-1 raftkv-2
```

### Performance Issues

```bash
# Check resource usage
kubectl top pods -n raftkv

# Check storage performance
kubectl exec -n raftkv raftkv-0 -- dd if=/dev/zero of=/var/lib/raftkv/test bs=1M count=100

# Check network latency between pods
kubectl exec -n raftkv raftkv-0 -- ping -c 10 raftkv-1.raftkv-headless.raftkv.svc.cluster.local
```

---

## Clean Up

### Remove Deployment

```bash
# Delete all resources
kubectl delete -k deployments/kubernetes/

# Or delete namespace (removes everything)
kubectl delete namespace raftkv
```

### Remove from Minikube

```bash
# Delete deployment
kubectl delete -k deployments/kubernetes/

# Remove image from Minikube
minikube image rm raftkv:latest

# Stop Minikube
minikube stop

# Delete Minikube cluster
minikube delete
```

---

## Quick Reference

### Essential Commands

#### Using Make (Recommended)

```bash
# Full deployment (build + load + install)
make k8s-deploy

# Check status
make k8s-status

# View logs
make k8s-logs

# Port-forward
make k8s-port-forward

# Upgrade after code changes
make k8s-upgrade

# Restart pods
make k8s-restart

# Clean everything
make k8s-clean

# Redeploy from scratch
make k8s-redeploy
```

#### Using Helper Script

```bash
# Navigate to kubernetes directory
cd deployments/kubernetes

# Full deployment
./deploy.sh deploy

# Check status
./deploy.sh status

# View logs
./deploy.sh logs [pod-name]

# Port-forward
./deploy.sh port-forward

# Upgrade after changes
./deploy.sh upgrade

# Restart pods
./deploy.sh restart

# Clean up everything
./deploy.sh clean

# Show help
./deploy.sh help
```

#### Manual Commands (Helm)

```bash
# Build and load image
make docker-build
minikube image load raftkv:latest

# Install with Helm
helm install raftkv ./deployments/helm/raftkv -n raftkv --create-namespace \
  -f ./deployments/helm/raftkv/values-development.yaml

# Validate
./deployments/kubernetes/validate.sh

# Access
kubectl port-forward -n raftkv svc/raftkv 8080:8080

# View logs
kubectl logs -n raftkv raftkv-0 -f

# Restart
kubectl rollout restart statefulset/raftkv -n raftkv

# Scale
helm upgrade raftkv ./deployments/helm/raftkv -n raftkv --reuse-values --set replicaCount=5

# Uninstall
helm uninstall raftkv -n raftkv
```

### Useful Aliases

```bash
# Add to ~/.bashrc or ~/.zshrc
alias k='kubectl'
alias kgp='kubectl get pods -n raftkv'
alias kgpw='kubectl get pods -n raftkv -w'
alias kl='kubectl logs -n raftkv'
alias kd='kubectl describe pod -n raftkv'
alias ke='kubectl exec -n raftkv'
alias kpf='kubectl port-forward -n raftkv svc/raftkv 8080:8080'
```

---

- **Documentation**: [../../docs/ARCHITECTURE.md](../../docs/ARCHITECTURE.md)
- **Kubernetes README**: [README.md](README.md)
- **GitHub Issues**: https://github.com/RashikAnsar/raftkv/issues
