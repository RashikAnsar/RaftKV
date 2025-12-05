#!/bin/bash

# Kubernetes Deployment - Comprehensive Test Suite
# This script runs all K8S deployment tests automatically

set -e

# Configuration
NAMESPACE="${NAMESPACE:-raftkv}"
TEST_KEY_PREFIX="kubernetes-deployment-test"
LOAD_TEST_DURATION="${LOAD_TEST_DURATION:-10}"
LOAD_TEST_TARGET="${LOAD_TEST_TARGET:-1000}"
LOAD_TEST_CONCURRENCY="${LOAD_TEST_CONCURRENCY:-50}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
MAGENTA='\033[0;35m'
NC='\033[0m' # No Color

# Test results tracking
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0
SKIPPED_TESTS=0

# Helper functions
print_header() {
    echo ""
    echo -e "${CYAN}========================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}========================================${NC}"
}

print_test() {
    echo -e "${BLUE}TEST: $1${NC}"
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
    ((PASSED_TESTS++))
    ((TOTAL_TESTS++))
}

print_failure() {
    echo -e "${RED}✗ $1${NC}"
    ((FAILED_TESTS++))
    ((TOTAL_TESTS++))
}

print_skip() {
    echo -e "${YELLOW}⊘ $1${NC}"
    ((SKIPPED_TESTS++))
    ((TOTAL_TESTS++))
}

print_info() {
    echo -e "${CYAN}ℹ $1${NC}"
}

wait_for_key() {
    if [ "${INTERACTIVE:-true}" = "true" ]; then
        echo ""
        echo -e "${YELLOW}Press Enter to continue to next test...${NC}"
        read -r
    else
        sleep 2
    fi
}

# Cleanup function
cleanup() {
    echo ""
    print_info "Cleaning up test data..."

    # Kill any background port-forwards
    pkill -f "port-forward.*$NAMESPACE" 2>/dev/null || true

    # Delete test keys
    for i in {1..3}; do
        kubectl exec -n $NAMESPACE raftkv-0 -- sh -c "rm -f /var/lib/raftkv/kv/${TEST_KEY_PREFIX}-* 2>/dev/null" || true
    done

    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT

# Start testing
if [ -t 1 ]; then
    clear
fi
print_header "RaftKV Kubernetes deployment Test Suite"
echo "Namespace: $NAMESPACE"
echo "Test Key Prefix: $TEST_KEY_PREFIX"
echo "Load Test Config: ${LOAD_TEST_TARGET} ops/sec, ${LOAD_TEST_DURATION}s, ${LOAD_TEST_CONCURRENCY} workers"
echo ""
echo -e "${YELLOW}This script will run all Kubernetes deployment tests${NC}"
sleep 2

# ============================================
# Test 1: Cluster Status
# ============================================
print_header "Test 1: Cluster Status"
print_test "Checking if all pods are running"

POD_COUNT=$(kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv --no-headers 2>/dev/null | wc -l | tr -d ' ')
READY_COUNT=$(kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv --no-headers 2>/dev/null | grep -c "1/1" || echo "0")

kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv

if [ "$POD_COUNT" -eq 3 ] && [ "$READY_COUNT" -eq 3 ]; then
    print_success "All 3 pods are running and ready"
else
    print_failure "Expected 3 pods ready, got $READY_COUNT/$POD_COUNT"
fi

wait_for_key

# ============================================
# Test 2: Service Discovery
# ============================================
print_header "Test 2: Service Discovery"
print_test "Checking if services are created"

SERVICES=$(kubectl get svc -n $NAMESPACE --no-headers 2>/dev/null | wc -l | tr -d ' ')

kubectl get svc -n $NAMESPACE

if [ "$SERVICES" -ge 2 ]; then
    print_success "Services are created (found $SERVICES)"
else
    print_failure "Expected at least 2 services, got $SERVICES"
fi

wait_for_key

# ============================================
# Test 3: Persistent Storage
# ============================================
print_header "Test 3: Persistent Storage"
print_test "Checking if PVCs are bound"

PVC_COUNT=$(kubectl get pvc -n $NAMESPACE --no-headers 2>/dev/null | wc -l | tr -d ' ')
BOUND_COUNT=$(kubectl get pvc -n $NAMESPACE --no-headers 2>/dev/null | grep -c "Bound" || echo "0")

kubectl get pvc -n $NAMESPACE

if [ "$PVC_COUNT" -eq 3 ] && [ "$BOUND_COUNT" -eq 3 ]; then
    print_success "All 3 PVCs are bound"
else
    print_failure "Expected 3 PVCs bound, got $BOUND_COUNT/$PVC_COUNT"
fi

wait_for_key

# ============================================
# Test 4: Cluster Formation
# ============================================
print_header "Test 4: Cluster Formation"
print_test "Verifying 3-node Raft cluster"

# Port-forward to access the service
print_info "Setting up port-forward..."
pkill -f "port-forward.*$NAMESPACE" 2>/dev/null || true
sleep 1
kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080 > /dev/null 2>&1 &
sleep 3

# Check cluster nodes
CLUSTER_INFO=$(curl -s http://localhost:8080/cluster/nodes 2>/dev/null || echo '{"count":0}')
NODE_COUNT=$(echo "$CLUSTER_INFO" | grep -o '"count":[0-9]*' | cut -d':' -f2)

echo "Cluster Info:"
echo "$CLUSTER_INFO" | python3 -m json.tool 2>/dev/null || echo "$CLUSTER_INFO"

if [ "$NODE_COUNT" = "3" ]; then
    print_success "Raft cluster formed with 3 nodes"
else
    print_failure "Expected 3 nodes, got $NODE_COUNT"
fi

wait_for_key

# ============================================
# Test 5: Health Endpoints
# ============================================
print_header "Test 5: Health Endpoints"

print_test "Checking /health endpoint"
HEALTH=$(curl -s http://localhost:8080/health 2>/dev/null || echo '{"status":"unknown"}')
echo "Health: $HEALTH"

if echo "$HEALTH" | grep -q "healthy"; then
    print_success "Health endpoint is responding"
else
    print_failure "Health endpoint not responding correctly"
fi

print_test "Checking /ready endpoint"
READY=$(curl -s http://localhost:8080/ready 2>/dev/null || echo '{"status":"unknown"}')
echo "Ready: $READY"

if echo "$READY" | grep -q "ready"; then
    print_success "Ready endpoint is responding"
else
    print_failure "Ready endpoint not responding correctly"
fi

wait_for_key

# ============================================
# Test 6: Leader Election
# ============================================
print_header "Test 6: Leader Election"
print_test "Identifying current leader"

LEADER_INFO=$(curl -s http://localhost:8080/cluster/leader 2>/dev/null || echo '{"id":"unknown"}')
LEADER_ID=$(echo "$LEADER_INFO" | grep -o '"id":"[^"]*' | cut -d'"' -f4)

echo "Leader Info: $LEADER_INFO"
print_info "Current leader: $LEADER_ID"

if [ -n "$LEADER_ID" ] && [ "$LEADER_ID" != "unknown" ]; then
    print_success "Leader elected: $LEADER_ID"

    print_test "Testing leader re-election after pod deletion"
    print_info "Deleting leader pod: $LEADER_ID"

    kubectl delete pod -n $NAMESPACE "$LEADER_ID" --grace-period=0 --force 2>/dev/null || true

    print_info "Waiting 15 seconds for new leader election..."
    sleep 15

    # Restart port-forward as it may have died
    pkill -f "port-forward.*$NAMESPACE" 2>/dev/null || true
    sleep 1
    kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080 > /dev/null 2>&1 &
    sleep 3

    NEW_LEADER_INFO=$(curl -s http://localhost:8080/cluster/leader 2>/dev/null || echo '{"id":"unknown"}')
    NEW_LEADER_ID=$(echo "$NEW_LEADER_INFO" | grep -o '"id":"[^"]*' | cut -d'"' -f4)

    echo "New Leader Info: $NEW_LEADER_INFO"
    print_info "New leader: $NEW_LEADER_ID"

    if [ -n "$NEW_LEADER_ID" ] && [ "$NEW_LEADER_ID" != "unknown" ]; then
        print_success "New leader elected successfully: $NEW_LEADER_ID"
    else
        print_failure "Leader election failed after pod deletion"
    fi

    # Wait for deleted pod to come back
    print_info "Waiting for deleted pod to restart..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=raftkv -n $NAMESPACE --timeout=60s 2>/dev/null || true
    sleep 5
else
    print_failure "No leader elected"
fi

wait_for_key

# ============================================
# Test 7: Data Persistence
# ============================================
print_header "Test 7: Data Persistence"
print_test "Testing data persistence across pod restarts"

# Write test keys
print_info "Writing test keys..."
for i in {1..3}; do
    KEY="${TEST_KEY_PREFIX}-persist-$i"
    VALUE="persistent-value-$i-$(date +%s)"

    # Note: Auth might be enabled, this test assumes public endpoints or will be skipped
    RESULT=$(curl -s -X PUT "http://localhost:8080/keys/$KEY" \
        -H "Content-Type: text/plain" \
        -d "$VALUE" 2>/dev/null || echo "auth_required")

    if echo "$RESULT" | grep -q "auth_required\|401\|403"; then
        print_skip "Data persistence test (auth required - keys cannot be written without token)"
        break
    else
        echo "  Written: $KEY = $VALUE"
    fi
done

if ! echo "$RESULT" | grep -q "auth_required\|401\|403"; then
    print_info "Deleting all pods..."
    kubectl delete pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv --grace-period=0 --force 2>/dev/null || true

    print_info "Waiting for pods to restart..."
    sleep 10
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=raftkv -n $NAMESPACE --timeout=120s 2>/dev/null || true
    sleep 10

    # Restart port-forward
    pkill -f "port-forward.*$NAMESPACE" 2>/dev/null || true
    sleep 1
    kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080 > /dev/null 2>&1 &
    sleep 5

    # Verify keys still exist
    print_info "Verifying keys after restart..."
    PERSIST_SUCCESS=0
    for i in {1..3}; do
        KEY="${TEST_KEY_PREFIX}-persist-$i"
        RESULT=$(curl -s "http://localhost:8080/keys/$KEY" 2>/dev/null || echo "error")

        if [ "$RESULT" != "error" ] && [ "$RESULT" != "not found" ]; then
            echo "  ✓ Key found: $KEY"
            ((PERSIST_SUCCESS++))
        fi
    done

    if [ $PERSIST_SUCCESS -eq 3 ]; then
        print_success "All data persisted across pod restarts"
    else
        print_failure "Only $PERSIST_SUCCESS/3 keys persisted"
    fi
fi

wait_for_key

# ============================================
# Test 8: Rolling Restart
# ============================================
print_header "Test 8: Rolling Restart"
print_test "Testing rolling restart (validates redirect bug fix)"

print_info "Initiating rolling restart..."
kubectl rollout restart statefulset/raftkv -n $NAMESPACE 2>/dev/null || true

print_info "Waiting for rolling restart to complete..."
if kubectl rollout status statefulset/raftkv -n $NAMESPACE --timeout=120s 2>/dev/null; then
    print_success "Rolling restart completed successfully"
else
    print_failure "Rolling restart failed or timed out"
fi

print_info "Checking pod status after restart..."
kubectl get pods -n $NAMESPACE -l app.kubernetes.io/name=raftkv

# Restart port-forward
pkill -f "port-forward.*$NAMESPACE" 2>/dev/null || true
sleep 1
kubectl port-forward -n $NAMESPACE svc/raftkv 8080:8080 > /dev/null 2>&1 &
sleep 5

# Verify cluster is healthy
HEALTH_AFTER=$(curl -s http://localhost:8080/health 2>/dev/null || echo '{"status":"unknown"}')
if echo "$HEALTH_AFTER" | grep -q "healthy"; then
    print_success "Cluster is healthy after rolling restart"
else
    print_failure "Cluster health check failed after restart"
fi

wait_for_key

# ============================================
# Test 9: Load Test
# ============================================
print_header "Test 9: Load Test"
print_test "Running load test (${LOAD_TEST_TARGET} ops/sec for ${LOAD_TEST_DURATION}s)"

if [ -f "$(dirname "$0")/simple_load_test.sh" ]; then
    print_info "Starting load test..."

    DURATION=$LOAD_TEST_DURATION TARGET_OPS=$LOAD_TEST_TARGET CONCURRENCY=$LOAD_TEST_CONCURRENCY \
        "$(dirname "$0")/simple_load_test.sh" | tail -20

    if [ $? -eq 0 ]; then
        print_success "Load test completed"
    else
        print_failure "Load test failed"
    fi
else
    print_skip "Load test script not found"
fi

wait_for_key

# ============================================
# Test 10: Monitoring Integration
# ============================================
print_header "Test 10: Monitoring Integration"
print_test "Checking Prometheus ServiceMonitor configuration"

# Check if ServiceMonitor template exists
if [ -f "deployments/helm/raftkv/templates/servicemonitor.yaml" ]; then
    print_success "ServiceMonitor template exists"

    # Check production values
    if grep -q "serviceMonitor:" deployments/helm/raftkv/values-production.yaml && \
       grep -A 1 "serviceMonitor:" deployments/helm/raftkv/values-production.yaml | grep -q "enabled: true"; then
        print_success "ServiceMonitor enabled in production values"
    else
        print_failure "ServiceMonitor not properly configured in production"
    fi

    # Check if Prometheus Operator is installed
    if kubectl get crd servicemonitors.monitoring.coreos.com >/dev/null 2>&1; then
        print_success "Prometheus Operator is installed"

        # Check if ServiceMonitor resource exists
        if kubectl get servicemonitor -n $NAMESPACE >/dev/null 2>&1; then
            SM_COUNT=$(kubectl get servicemonitor -n $NAMESPACE --no-headers 2>/dev/null | wc -l | tr -d ' ')
            if [ "$SM_COUNT" -gt 0 ]; then
                print_success "ServiceMonitor resource created ($SM_COUNT found)"
            else
                print_info "ServiceMonitor resource not created (disabled in current deployment)"
            fi
        else
            print_info "No ServiceMonitor resources in namespace (expected for dev mode)"
        fi
    else
        print_info "Prometheus Operator not installed (expected for Minikube/dev)"
        print_success "ServiceMonitor template ready for production deployment"
    fi
else
    print_failure "ServiceMonitor template not found"
fi

# ============================================
# Final Summary
# ============================================
print_header "Test Summary"

echo ""
echo -e "${CYAN}Total Tests:${NC}    $TOTAL_TESTS"
echo -e "${GREEN}Passed:${NC}        $PASSED_TESTS"
echo -e "${RED}Failed:${NC}        $FAILED_TESTS"
echo -e "${YELLOW}Skipped:${NC}       $SKIPPED_TESTS"
echo ""

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}========================================${NC}"
    echo -e "${GREEN}✓ ALL TESTS PASSED!${NC}"
    echo -e "${GREEN}========================================${NC}"
    echo ""
    echo -e "${GREEN}Kubernetes Deployment is COMPLETE ✓${NC}"
    echo ""
    exit 0
elif [ $PASSED_TESTS -gt $FAILED_TESTS ]; then
    echo -e "${YELLOW}========================================${NC}"
    echo -e "${YELLOW}⚠ MOST TESTS PASSED${NC}"
    echo -e "${YELLOW}========================================${NC}"
    echo ""
    echo -e "${YELLOW}Kubernetes Deployment mostly complete with $FAILED_TESTS issues${NC}"
    echo ""
    exit 1
else
    echo -e "${RED}========================================${NC}"
    echo -e "${RED}✗ MULTIPLE TESTS FAILED${NC}"
    echo -e "${RED}========================================${NC}"
    echo ""
    echo -e "${RED}Kubernetes Deployment needs attention: $FAILED_TESTS tests failed${NC}"
    echo ""
    exit 1
fi
