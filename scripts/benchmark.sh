#!/bin/bash

# Benchmark runner script for RaftKV
# Usage: ./scripts/benchmark.sh [options]
#
# Options:
#   -t, --type TYPE     Benchmark type: storage, server, cluster, all (default: all)
#   -o, --output FILE   Output file for results (default: stdout)
#   -c, --count N       Number of benchmark iterations (default: 1)
#   -m, --memory        Include memory profiling
#   -p, --cpu           Include CPU profiling
#   -h, --help          Show this help message

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
        -m|--memory)
            MEMORY_PROFILE=true
            shift
            ;;
        -p|--cpu)
            CPU_PROFILE=true
            shift
            ;;
        -h|--help)
            head -n 12 "$0" | tail -n 11
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
    storage|server|cluster|all)
        ;;
    *)
        echo -e "${RED}Invalid benchmark type: $BENCH_TYPE${NC}"
        echo "Valid types: storage, server, cluster, all"
        exit 1
        ;;
esac

# Create output directory for profiles
mkdir -p benchmarks/profiles

echo -e "${BLUE}=== RaftKV Benchmark Suite ===${NC}"
echo -e "Type: ${GREEN}$BENCH_TYPE${NC}"
echo -e "Iterations: ${GREEN}$BENCH_COUNT${NC}"
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

    echo -e "${YELLOW}Running $bench_name benchmarks...${NC}"

    if [ -n "$OUTPUT_FILE" ]; then
        go test ./test/benchmark -run=^$ $BENCH_FLAGS -bench="$bench_pattern" | tee -a "$OUTPUT_FILE"
    else
        go test ./test/benchmark -run=^$ $BENCH_FLAGS -bench="$bench_pattern"
    fi

    echo ""
}

# Run benchmarks based on type
case $BENCH_TYPE in
    storage)
        run_benchmark "Storage" "BenchmarkMemoryStore|BenchmarkDurableStore|BenchmarkSnapshot"
        ;;
    server)
        run_benchmark "Server" "BenchmarkHTTPServer|BenchmarkGRPCServer|BenchmarkHTTPvsGRPC"
        ;;
    cluster)
        run_benchmark "Cluster" "BenchmarkCluster"
        ;;
    all)
        run_benchmark "Storage" "BenchmarkMemoryStore|BenchmarkDurableStore|BenchmarkSnapshot"
        run_benchmark "Server" "BenchmarkHTTPServer|BenchmarkGRPCServer|BenchmarkHTTPvsGRPC"
        run_benchmark "Cluster" "BenchmarkCluster"
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
    echo -e "${YELLOW}=== Summary ===${NC}"
    echo "Storage Performance:"
    grep "ops/sec" "$OUTPUT_FILE" | grep -E "BenchmarkMemoryStore|BenchmarkDurableStore" | head -5
    echo ""
    echo "Server Performance:"
    grep "req/sec" "$OUTPUT_FILE" | grep -E "BenchmarkHTTPServer|BenchmarkGRPCServer" | head -5
    echo ""
    echo "Cluster Performance:"
    grep -E "writes/sec|reads/sec|ops/sec" "$OUTPUT_FILE" | grep "BenchmarkCluster" | head -5
fi
