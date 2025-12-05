# RaftKV Testing Suite

This directory contains testing scripts for RaftKV.

## Kuberented Deployment Test Suite

Comprehensive automated tests for Kubernetes Deployment.

### Quick Start

```bash
# Run all k8s deployment tests (interactive mode)
./test/kubernetes_deployment_tests.sh

# Run all k8s deployment tests (non-interactive mode)
INTERACTIVE=false ./test/kubernetes_deployment_tests.sh

# Using make
make test-kubernetes-deployment
```

### Test Coverage

The `kubernetes_deployment_tests.sh` script runs 10 comprehensive tests:

1. **Cluster Status** - Verifies all 3 pods are running and ready
2. **Service Discovery** - Checks if services are created
3. **Persistent Storage** - Verifies PVCs are bound
4. **Cluster Formation** - Confirms 3-node Raft cluster
5. **Health Endpoints** - Tests `/health` and `/ready` endpoints
6. **Leader Election** - Tests leader election and re-election after pod deletion
7. **Data Persistence** - Verifies data persists across pod restarts
8. **Rolling Restart** - Tests rolling restart (validates redirect bug fix)
9. **Load Test** - Runs load test for performance validation
10. **Monitoring Integration** - Checks Prometheus ServiceMonitor configuration

### Configuration

Environment variables:

```bash
# Kubernetes namespace (default: raftkv)
NAMESPACE=raftkv

# Test key prefix for data persistence tests (default: kubernetes-deployment-test)
TEST_KEY_PREFIX=kubernetes-deployment-test

# Load test configuration
LOAD_TEST_DURATION=10      # seconds (default: 10)
LOAD_TEST_TARGET=1000      # ops/sec (default: 1000)
LOAD_TEST_CONCURRENCY=50   # workers (default: 50)

# Interactive mode (default: true)
INTERACTIVE=false          # Set to false to skip "press enter" prompts
```

### Examples

```bash
# Quick test (non-interactive, lighter load test)
INTERACTIVE=false LOAD_TEST_DURATION=5 LOAD_TEST_TARGET=500 \
  ./test/kubernetes_deployment_test.sh

# Heavy load test
LOAD_TEST_DURATION=30 LOAD_TEST_TARGET=5000 LOAD_TEST_CONCURRENCY=100 \
  ./test/kubernetes_deployment_test.sh

# Test specific namespace
NAMESPACE=my-raftkv ./test/kubernetes_deployment_test.sh
```

### Expected Output

The script provides color-coded output:

- 🔵 **Blue** - Test names and info messages
- ✅ **Green** - Successful tests
- ❌ **Red** - Failed tests
- ⊘ **Yellow** - Skipped tests (e.g., auth required)

Final summary shows:
- Total tests run
- Number passed/failed/skipped
- Overall status

### Exit Codes

- `0` - All tests passed
- `1` - Some tests failed but majority passed
- `1` - Multiple critical failures

## Load Testing

### Simple Load Test

Uses health endpoints (no authentication required):

```bash
# Default: 1000 ops/sec for 10 seconds
./test/simple_load_test.sh

# Custom configuration
DURATION=30 TARGET_OPS=5000 CONCURRENCY=100 \
  ./test/simple_load_test.sh
```

### Full Load Test

Requires authentication (tests PUT/GET/DELETE operations):

```bash
./test/load_test.sh
```

**Note:** If authentication is enabled and you don't have credentials, use `simple_load_test.sh` instead.

### Load Test Results

Load test results are limited by kubectl port-forward overhead:
- Through port-forward: ~200-500 req/sec
- Direct cluster access: 10K+ req/sec (use existing benchmark tests)

## Benchmark Tests

Go-based benchmark tests for accurate performance measurements:

```bash
# Run all benchmarks
go test -bench=. ./test/benchmark/...

# Specific benchmarks
go test -bench=BenchmarkBatchWrite ./test/benchmark/
go test -bench=BenchmarkServerHTTP ./test/benchmark/
```

## Integration Tests

Located in `test/integration/`:

```bash
# Run all integration tests
go test ./test/integration/...

# With verbose output
go test -v ./test/integration/...
```

## Troubleshooting

### Port-Forward Issues

If tests fail with connection errors:

```bash
# Kill existing port-forwards
pkill -f "port-forward.*raftkv"

# Restart the test
./test/kubernetes_deployment_test.sh
```

### Pod Not Ready

If pods aren't ready:

```bash
# Check pod status
kubectl get pods -n raftkv

# Check logs
kubectl logs -n raftkv raftkv-0

# Wait for pods
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=raftkv -n raftkv --timeout=120s
```

### Authentication Errors

Some tests require unauthenticated endpoints. If you see "auth_required" errors:
- Tests will automatically skip authenticated operations
- Use `simple_load_test.sh` instead of `load_test.sh`
- This is expected behavior and doesn't indicate a failure

## CI/CD Integration

For continuous integration:

```bash
# Run in CI mode (non-interactive, fail fast)
INTERACTIVE=false ./test/kubernetes_deployment_test.sh
EXIT_CODE=$?

# Check exit code
if [ $EXIT_CODE -eq 0 ]; then
  echo "All tests passed"
else
  echo "Tests failed with code $EXIT_CODE"
  exit $EXIT_CODE
fi
```

## Adding New Tests

To add new tests to `kubernetes_deployment_test.sh`:

1. Add a new test section following the pattern:
```bash
print_header "Test N: Your Test Name"
print_test "Description of what you're testing"

# Your test logic here

if [ test_passed ]; then
    print_success "Test passed message"
else
    print_failure "Test failed message"
fi

wait_for_key
```

2. Update the test count in this README
3. Document any new environment variables

## See Also

- [Kubernetes Deployment README](../deployments/kubernetes/README.md)
- [Build and Deploy Guide](../deployments/kubernetes/BUILD_AND_DEPLOY.md)
- [Architecture Documentation](../docs/ARCHITECTURE.md)
