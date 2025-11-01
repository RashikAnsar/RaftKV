#!/bin/bash
# Quick script to test gRPC connectivity

set -e

echo "=== Testing gRPC Connectivity ==="
echo ""

# Function to test gRPC port
test_grpc_port() {
    local port=$1
    local node=$2

    echo "Testing $node (gRPC port $port)..."

    # Check if port is listening
    if nc -z localhost $port 2>/dev/null; then
        echo "  Port $port is open"

        # Try to list gRPC services (requires grpcurl)
        if command -v grpcurl &> /dev/null; then
            echo "  Testing gRPC service..."
            if grpcurl -plaintext localhost:$port list &> /dev/null; then
                echo "  gRPC server is responding"
                grpcurl -plaintext localhost:$port list
            else
                echo "  gRPC server not responding properly"
            fi
        else
            echo "  grpcurl not installed, skipping service test"
            echo "     Install with: brew install grpcurl"
        fi
    else
        echo "  Port $port is not open"
        echo "     Server may not be running or gRPC not enabled"
    fi
    echo ""
}

# Test all three nodes
test_grpc_port 9091 "node1"
test_grpc_port 9092 "node2"
test_grpc_port 9093 "node3"

# Check if processes are running
echo "=== Checking Running Processes ==="
if pgrep -f "kvstore.*raft" > /dev/null; then
    echo "kvstore processes are running:"
    pgrep -lf "kvstore.*raft"
    echo ""
else
    echo "No kvstore processes found"
    echo ""
    echo "Start the cluster with:"
    echo "  make raft-node1  # Terminal 1"
    echo "  make raft-node2  # Terminal 2"
    echo "  make raft-node3  # Terminal 3"
    echo ""
    exit 1
fi

# Test with CLI if available
if [ -f "./bin/kvcli" ]; then
    echo "=== Testing CLI (gRPC with Auto-Configuration) ==="

    # Test 1: Using HTTP port (auto-converts to gRPC)
    echo "Test 1: Using HTTP port 8081 (auto-converts to gRPC)..."
    if ./bin/kvcli --protocol=grpc --server=localhost:8081 put test-grpc "hello from grpc" 2>&1 | grep -q "Put key"; then
        echo "  Put succeeded (HTTP port auto-converted to gRPC)"

        # Try to get it back
        if ./bin/kvcli --protocol=grpc --server=localhost:8081 get test-grpc 2>&1 | grep -q "hello from grpc"; then
            echo "  Get succeeded"
        else
            echo "  Get failed"
        fi
    else
        echo "  Put failed"
        echo ""
        echo "Common issues:"
        echo "1. Server not fully started - wait a few more seconds"
        echo "2. gRPC not enabled - check server was started with --grpc flag"
        echo "3. Wrong port - verify server is listening on 9091"
    fi
    echo ""

    # Test 2: Using gRPC port directly
    echo "Test 2: Using gRPC port 9091 directly..."
    if ./bin/kvcli --protocol=grpc --server=localhost:9091 put test-grpc2 "direct grpc" 2>&1 | grep -q "Put key"; then
        echo "  Put succeeded (direct gRPC port)"

        if ./bin/kvcli --protocol=grpc --server=localhost:9091 get test-grpc2 2>&1 | grep -q "direct grpc"; then
            echo "  Get succeeded"
        else
            echo "  Get failed"
        fi
    else
        echo "  Put failed"
    fi
    echo ""

    # Test 3: Stats command
    echo "Test 3: Stats command via gRPC..."
    if ./bin/kvcli --protocol=grpc --server=localhost:8081 stats 2>&1 | grep -q "Server Statistics"; then
        echo "  Stats command succeeded"
    else
        echo "  Stats command failed"
    fi
fi

echo ""
echo "=== Diagnostic Complete ==="
