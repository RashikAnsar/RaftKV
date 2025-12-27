#!/bin/bash

# Benchmark runner script for RaftKV
# Usage: ./scripts/benchmark.sh [options]
#
# Options:
#   -t, --type TYPE        Benchmark type: storage, server, batch, cluster, all (default: all)
#   -o, --output FILE      Output file for results (default: stdout)
#   -c, --count N          Number of benchmark iterations (default: 1)
#   -b, --benchtime TIME   Benchmark time per test (default: auto - 1s for most, 500ms for cluster)
#   -m, --memory           Include memory profiling
#   -p, --cpu              Include CPU profiling
#   -h, --help             Show this help message

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
BENCH_TYPE="all"
OUTPUT_FILE=""
BENCH_COUNT=1
BENCH_TIME=""  # Empty means auto-select based on type
MEMORY_PROFILE=false
CPU_PROFILE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -t|--type)
            BENCH_TYPE="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_FILE="$2"
            shift 2
            ;;
        -c|--count)
            BENCH_COUNT="$2"
            shift 2
            ;;
        -b|--benchtime)
            BENCH_TIME="$2"
            shift 2
            ;;
        -m|--memory)
            MEMORY_PROFILE=true
            shift
            ;;
        -p|--cpu)
            CPU_PROFILE=true
            shift
            ;;
        -h|--help)
            head -n 13 "$0" | tail -n 12
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# Validate benchmark type
case $BENCH_TYPE in
    storage|server|batch|cluster|all)
        ;;
    *)
        echo -e "${RED}Invalid benchmark type: $BENCH_TYPE${NC}"
        echo "Valid types: storage, server, batch, cluster, all"
        exit 1
        ;;
esac

# Create output directory for profiles
mkdir -p benchmarks/profiles

echo -e "${BLUE}=== RaftKV Benchmark Suite ===${NC}"
echo -e "Type: ${GREEN}$BENCH_TYPE${NC}"
echo -e "Iterations: ${GREEN}$BENCH_COUNT${NC}"
echo -e "Benchtime: ${GREEN}${BENCH_TIME:-auto}${NC}"
echo -e "Memory Profile: ${GREEN}$MEMORY_PROFILE${NC}"
echo -e "CPU Profile: ${GREEN}$CPU_PROFILE${NC}"
echo ""

# Build benchmark flags
BENCH_FLAGS="-bench=. -benchmem -count=$BENCH_COUNT -timeout=30m"

if [ "$MEMORY_PROFILE" = true ]; then
    BENCH_FLAGS="$BENCH_FLAGS -memprofile=benchmarks/profiles/mem.out"
fi

if [ "$CPU_PROFILE" = true ]; then
    BENCH_FLAGS="$BENCH_FLAGS -cpuprofile=benchmarks/profiles/cpu.out"
fi

# Function to run benchmarks
run_benchmark() {
    local bench_name=$1
    local bench_pattern=$2
    local benchtime=${3:-"1s"}  # Default benchtime, can be overridden

    echo -e "${YELLOW}Running $bench_name benchmarks...${NC}"

    # Build flags for this specific benchmark run
    local flags="$BENCH_FLAGS -benchtime=$benchtime"

    if [ -n "$OUTPUT_FILE" ]; then
        go test ./test/benchmark -run=^$ $flags -bench="$bench_pattern" | tee -a "$OUTPUT_FILE"
    else
        go test ./test/benchmark -run=^$ $flags -bench="$bench_pattern"
    fi

    echo ""
}

# Determine benchtime based on type if not specified
if [ -z "$BENCH_TIME" ]; then
    if [ "$BENCH_TYPE" = "cluster" ]; then
        BENCH_TIME="500ms"  # Faster default for cluster benchmarks
    else
        BENCH_TIME="1s"     # Standard default
    fi
fi

# Run benchmarks based on type
case $BENCH_TYPE in
    storage)
        run_benchmark "Storage" "BenchmarkStorage|BenchmarkValueSizes|BenchmarkSnapshot|BenchmarkRecovery" "$BENCH_TIME"
        ;;
    server)
        run_benchmark "Server" "BenchmarkHTTPServer|BenchmarkCachedVsUncached" "$BENCH_TIME"
        ;;
    batch)
        run_benchmark "Batch Writes" "BenchmarkBatchedWrites" "$BENCH_TIME"
        ;;
    cluster)
        run_benchmark "Cluster" "BenchmarkCluster" "$BENCH_TIME"
        ;;
    all)
        run_benchmark "Storage" "BenchmarkStorage|BenchmarkValueSizes|BenchmarkSnapshot|BenchmarkRecovery" "$BENCH_TIME"
        run_benchmark "Batch Writes" "BenchmarkBatchedWrites" "$BENCH_TIME"
        run_benchmark "Server" "BenchmarkHTTPServer|BenchmarkCachedVsUncached" "$BENCH_TIME"
        echo ""
        echo -e "${YELLOW}Note: Cluster benchmarks are slow and skipped in 'all'. Run with -t cluster to benchmark cluster operations.${NC}"
        ;;
esac

echo -e "${GREEN}✓ Benchmarks completed${NC}"

# Show profile analysis instructions if profiles were generated
if [ "$MEMORY_PROFILE" = true ] || [ "$CPU_PROFILE" = true ]; then
    echo ""
    echo -e "${YELLOW}Profile Analysis:${NC}"

    if [ "$CPU_PROFILE" = true ]; then
        echo -e "  CPU: ${BLUE}go tool pprof benchmarks/profiles/cpu.out${NC}"
        echo -e "       Interactive: top, list, web"
    fi

    if [ "$MEMORY_PROFILE" = true ]; then
        echo -e "  Memory: ${BLUE}go tool pprof benchmarks/profiles/mem.out${NC}"
        echo -e "       Interactive: top, list, web"
    fi
fi

# Generate summary if output file was specified
if [ -n "$OUTPUT_FILE" ]; then
    echo ""
    echo -e "${GREEN}Results saved to: $OUTPUT_FILE${NC}"

    # Extract key metrics
    echo ""
    echo -e "${YELLOW}=== Performance Summary ===${NC}"
    echo ""

    echo -e "${BLUE}Storage Layer:${NC}"
    grep -E "BenchmarkStorage.*-14" "$OUTPUT_FILE" | grep -E "Get|Put|Delete" | head -6 || echo "  No storage benchmarks found"
    echo ""

    echo -e "${BLUE}HTTP API:${NC}"
    grep -E "BenchmarkHTTPServer_(Sequential|Concurrent)" "$OUTPUT_FILE" | grep -E "GET|PUT|Workers" | head -6 || echo "  No server benchmarks found"
    echo ""

    echo -e "${BLUE}Cache Performance:${NC}"
    grep -E "BenchmarkCachedVsUncached" "$OUTPUT_FILE" | head -2 || echo "  No cache benchmarks found"
    echo ""

    echo -e "${BLUE}Batch Writes:${NC}"
    grep -E "BenchmarkBatchedWrites" "$OUTPUT_FILE" | grep -E "ops/sec" | head -3 || echo "  No batch benchmarks found"
    echo ""

    # Extract top performance numbers
    echo -e "${YELLOW}=== Top Performance Metrics ===${NC}"

    # Storage ops/sec
    STORAGE_OPS=$(grep -oE "[0-9.]+ ops/sec" "$OUTPUT_FILE" | head -1 | awk '{print $1}')
    if [ -n "$STORAGE_OPS" ]; then
        echo -e "  Storage: ${GREEN}${STORAGE_OPS}${NC} ops/sec"
    fi

    # HTTP req/sec
    HTTP_REQ=$(grep -oE "[0-9.]+ req/sec" "$OUTPUT_FILE" | sort -rn | head -1 | awk '{print $1}')
    if [ -n "$HTTP_REQ" ]; then
        echo -e "  HTTP API: ${GREEN}${HTTP_REQ}${NC} req/sec"
    fi

    # Find peak throughput from Workers
    PEAK_WORKERS=$(grep "Workers-" "$OUTPUT_FILE" | grep -oE "[0-9.]+ req/sec" | sort -rn | head -1 | awk '{print $1}')
    if [ -n "$PEAK_WORKERS" ]; then
        echo -e "  Peak (concurrent): ${GREEN}${PEAK_WORKERS}${NC} req/sec"
    fi
fi
