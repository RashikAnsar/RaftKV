#!/bin/bash

# RaftKV Load Test Script
# Target: 10,000 operations per second
# Tests: PUT, GET, DELETE operations

set -e

# Configuration
HOST="${RAFTKV_HOST:-http://localhost:8080}"
DURATION="${DURATION:-30}"  # seconds
TARGET_OPS="${TARGET_OPS:-10000}"  # ops/sec
CONCURRENCY="${CONCURRENCY:-100}"  # parallel workers

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo "========================================"
echo "RaftKV Load Test"
echo "========================================"
echo "Host: $HOST"
echo "Duration: ${DURATION}s"
echo "Target: ${TARGET_OPS} ops/sec"
echo "Concurrency: ${CONCURRENCY} workers"
echo "========================================"

# Get JWT token
echo -e "${BLUE}Getting authentication token...${NC}"
TOKEN=$(curl -s -X POST "$HOST/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin"}' | grep -o '"token":"[^"]*' | cut -d'"' -f4)

if [ -z "$TOKEN" ]; then
  echo -e "${RED}Failed to get authentication token${NC}"
  exit 1
fi
echo -e "${GREEN}Token acquired${NC}"

# Warmup
echo -e "${BLUE}Warming up...${NC}"
for i in {1..100}; do
  curl -s -X PUT "$HOST/keys/warmup-$i" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: text/plain" \
    -d "warmup-value-$i" > /dev/null
done
echo -e "${GREEN}Warmup complete${NC}"

# Function to run PUT operations
run_put_test() {
  local ops_per_worker=$1
  local worker_id=$2
  local count=0
  local start_time=$(date +%s)

  for ((i=0; i<ops_per_worker; i++)); do
    key="load-test-w${worker_id}-${i}"
    value="value-${RANDOM}"

    curl -s -X PUT "$HOST/keys/$key" \
      -H "Authorization: Bearer $TOKEN" \
      -H "Content-Type: text/plain" \
      -d "$value" > /dev/null 2>&1

    ((count++))
  done

  local end_time=$(date +%s)
  local elapsed=$((end_time - start_time))
  echo "$count,$elapsed"
}

# Function to run GET operations
run_get_test() {
  local ops_per_worker=$1
  local worker_id=$2
  local count=0
  local start_time=$(date +%s)

  for ((i=0; i<ops_per_worker; i++)); do
    key="load-test-w${worker_id}-$((RANDOM % ops_per_worker))"

    curl -s -X GET "$HOST/keys/$key" \
      -H "Authorization: Bearer $TOKEN" > /dev/null 2>&1

    ((count++))
  done

  local end_time=$(date +%s)
  local elapsed=$((end_time - start_time))
  echo "$count,$elapsed"
}

export -f run_put_test
export -f run_get_test
export HOST
export TOKEN

# Calculate operations per worker
OPS_PER_WORKER=$((TARGET_OPS * DURATION / CONCURRENCY))

echo ""
echo "========================================"
echo "Running PUT Load Test"
echo "========================================"
echo "Operations per worker: $OPS_PER_WORKER"
echo "Starting $CONCURRENCY workers..."

# Run PUT test
start_time=$(date +%s.%N)

for ((i=0; i<CONCURRENCY; i++)); do
  run_put_test $OPS_PER_WORKER $i &
done

# Wait for all workers
wait

end_time=$(date +%s.%N)
elapsed=$(echo "$end_time - $start_time" | bc)

total_ops=$((TARGET_OPS * DURATION))
actual_ops_per_sec=$(echo "scale=2; $total_ops / $elapsed" | bc)

echo ""
echo -e "${GREEN}PUT Test Results:${NC}"
echo "Total operations: $total_ops"
echo "Total time: ${elapsed}s"
echo "Throughput: ${actual_ops_per_sec} ops/sec"

if (( $(echo "$actual_ops_per_sec >= $TARGET_OPS" | bc -l) )); then
  echo -e "${GREEN}✓ Target achieved!${NC}"
  put_success=1
else
  echo -e "${YELLOW}⚠ Target not met (${actual_ops_per_sec}/${TARGET_OPS} ops/sec)${NC}"
  put_success=0
fi

# Short pause between tests
sleep 2

echo ""
echo "========================================"
echo "Running GET Load Test"
echo "========================================"
echo "Operations per worker: $OPS_PER_WORKER"
echo "Starting $CONCURRENCY workers..."

# Run GET test
start_time=$(date +%s.%N)

for ((i=0; i<CONCURRENCY; i++)); do
  run_get_test $OPS_PER_WORKER $i &
done

# Wait for all workers
wait

end_time=$(date +%s.%N)
elapsed=$(echo "$end_time - $start_time" | bc)

actual_ops_per_sec=$(echo "scale=2; $total_ops / $elapsed" | bc)

echo ""
echo -e "${GREEN}GET Test Results:${NC}"
echo "Total operations: $total_ops"
echo "Total time: ${elapsed}s"
echo "Throughput: ${actual_ops_per_sec} ops/sec"

if (( $(echo "$actual_ops_per_sec >= $TARGET_OPS" | bc -l) )); then
  echo -e "${GREEN}✓ Target achieved!${NC}"
  get_success=1
else
  echo -e "${YELLOW}⚠ Target not met (${actual_ops_per_sec}/${TARGET_OPS} ops/sec)${NC}"
  get_success=0
fi

# Cleanup
echo ""
echo -e "${BLUE}Cleaning up test data...${NC}"
for ((i=0; i<CONCURRENCY; i++)); do
  for ((j=0; j<10; j++)); do  # Delete first 10 keys from each worker
    curl -s -X DELETE "$HOST/keys/load-test-w${i}-${j}" \
      -H "Authorization: Bearer $TOKEN" > /dev/null 2>&1
  done
done
echo -e "${GREEN}Cleanup complete${NC}"

# Final summary
echo ""
echo "========================================"
echo "Load Test Summary"
echo "========================================"
if [ $put_success -eq 1 ] && [ $get_success -eq 1 ]; then
  echo -e "${GREEN}✓ All tests passed!${NC}"
  exit 0
else
  echo -e "${YELLOW}⚠ Some tests did not meet the target${NC}"
  exit 1
fi
