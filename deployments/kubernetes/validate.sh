#!/bin/bash
# RaftKV Kubernetes Deployment Validation Script

set -e

NAMESPACE="raftkv"
TIMEOUT=300  # 5 minutes

echo "========================================="
echo "RaftKV Kubernetes Deployment Validation"
echo "========================================="
echo ""

# Color codes
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    echo -e "${RED}✗ kubectl is not installed${NC}"
    exit 1
fi
echo -e "${GREEN}✓ kubectl is installed${NC}"

# Check if cluster is accessible
if ! kubectl cluster-info &> /dev/null; then
    echo -e "${RED}✗ Cannot connect to Kubernetes cluster${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Kubernetes cluster is accessible${NC}"

# Check if namespace exists
if ! kubectl get namespace $NAMESPACE &> /dev/null; then
    echo -e "${RED}✗ Namespace '$NAMESPACE' does not exist${NC}"
    echo "Run: kubectl apply -k ."
    exit 1
fi
echo -e "${GREEN}✓ Namespace '$NAMESPACE' exists${NC}"

# Check if StatefulSet exists
if ! kubectl get statefulset raftkv -n $NAMESPACE &> /dev/null; then
    echo -e "${RED}✗ StatefulSet 'raftkv' does not exist${NC}"
    exit 1
fi
echo -e "${GREEN}✓ StatefulSet 'raftkv' exists${NC}"

# Check desired replicas
DESIRED_REPLICAS=$(kubectl get statefulset raftkv -n $NAMESPACE -o jsonpath='{.spec.replicas}')
echo -e "${GREEN}✓ Desired replicas: $DESIRED_REPLICAS${NC}"

# Wait for pods to be ready
echo ""
echo "Waiting for pods to be ready (timeout: ${TIMEOUT}s)..."
if ! kubectl wait --for=condition=ready pod -l app=raftkv -n $NAMESPACE --timeout=${TIMEOUT}s 2>&1 | grep -v "no matching resources found"; then
    echo -e "${RED}✗ Pods did not become ready in time${NC}"
    echo ""
    echo "Pod status:"
    kubectl get pods -n $NAMESPACE
    echo ""
    echo "Recent events:"
    kubectl get events -n $NAMESPACE --sort-by='.lastTimestamp' | tail -10
    exit 1
fi

# Check pod status
echo ""
echo "Pod Status:"
kubectl get pods -n $NAMESPACE -o wide

READY_PODS=$(kubectl get pods -n $NAMESPACE -l app=raftkv -o jsonpath='{.items[?(@.status.phase=="Running")].metadata.name}' | wc -w | tr -d ' ')
echo ""
if [ "$READY_PODS" -eq "$DESIRED_REPLICAS" ]; then
    echo -e "${GREEN}✓ All $DESIRED_REPLICAS pods are running${NC}"
else
    echo -e "${RED}✗ Only $READY_PODS/$DESIRED_REPLICAS pods are running${NC}"
    exit 1
fi

# Check services
echo ""
echo "Services:"
kubectl get svc -n $NAMESPACE

# Test health endpoint from each pod
echo ""
echo "Testing health endpoints..."
for i in 0 1 2; do
    POD_NAME="raftkv-$i"
    if kubectl get pod $POD_NAME -n $NAMESPACE &> /dev/null; then
        HEALTH=$(kubectl exec -n $NAMESPACE $POD_NAME -- curl -s http://localhost:8080/health 2>/dev/null || echo "failed")
        if [ "$HEALTH" != "failed" ]; then
            echo -e "${GREEN}✓ $POD_NAME: Health check OK${NC}"
        else
            echo -e "${RED}✗ $POD_NAME: Health check failed${NC}"
        fi
    fi
done

# Check cluster nodes
echo ""
echo "Checking RaftKV cluster status..."
LEADER_POD=$(kubectl exec -n $NAMESPACE raftkv-0 -- curl -s http://localhost:8080/cluster/leader 2>/dev/null | grep -o 'raftkv-[0-9]' | head -1 || echo "unknown")

if [ "$LEADER_POD" != "unknown" ]; then
    echo -e "${GREEN}✓ Cluster leader: $LEADER_POD${NC}"

    # Get cluster nodes
    echo ""
    echo "Cluster nodes:"
    kubectl exec -n $NAMESPACE raftkv-0 -- curl -s http://localhost:8080/cluster/nodes 2>/dev/null | grep -E '"id"|"role"' || echo "Could not retrieve cluster info"
else
    echo -e "${YELLOW}⚠ Could not determine cluster leader (cluster may still be forming)${NC}"
fi

# Check PVCs
echo ""
echo "Persistent Volume Claims:"
kubectl get pvc -n $NAMESPACE

# Check storage usage
echo ""
echo "Storage Usage:"
for i in 0 1 2; do
    POD_NAME="raftkv-$i"
    if kubectl get pod $POD_NAME -n $NAMESPACE &> /dev/null; then
        USAGE=$(kubectl exec -n $NAMESPACE $POD_NAME -- df -h /var/lib/raftkv 2>/dev/null | tail -1 | awk '{print $5}' || echo "N/A")
        echo "$POD_NAME: $USAGE used"
    fi
done

# Summary
echo ""
echo "========================================="
echo -e "${GREEN}Validation Complete!${NC}"
echo "========================================="
echo ""
echo "Next steps:"
echo "1. Port-forward: kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080"
echo "2. Test API: curl http://localhost:8080/cluster/nodes"
echo "3. View logs: kubectl logs -n $NAMESPACE raftkv-0 -f"
echo "4. Access Grafana: kubectl port-forward -n monitoring svc/grafana 3000:3000"
echo ""
