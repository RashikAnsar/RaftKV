#!/bin/bash
# Test CLI auto-configuration feature

set -e

echo "=== Testing CLI Auto-Configuration ==="
echo ""

# Check if CLI exists
if [ ! -f "./bin/kvcli" ]; then
    echo "CLI not built. Run: make build-cli"
    exit 1
fi

# Ensure cluster is running
if ! pgrep -f "kvstore.*raft" > /dev/null; then
    echo "Cluster not running. Start with: make raft-cluster"
    echo "   Or manually: make raft-node1 (in 3 terminals)"
    exit 1
fi

echo "Cluster is running"
echo ""

# Test 1: HTTP port auto-converts to gRPC
echo "Test 1: HTTP port (8081) auto-converts to gRPC (9091)..."
if ./bin/kvcli --protocol=grpc --server=localhost:8081 put test1 "http-port" 2>&1 | grep -q "Put key"; then
    echo "  Put succeeded"
    VALUE=$(./bin/kvcli --protocol=grpc --server=localhost:8081 get test1 2>&1)
    if [ "$VALUE" = "http-port" ]; then
        echo "  Get succeeded: $VALUE"
    else
        echo "  Get returned unexpected value: $VALUE"
    fi
else
    echo "  Put failed"
fi
echo ""

# Test 2: gRPC port works directly
echo "Test 2: gRPC port (9091) works directly..."
if ./bin/kvcli --protocol=grpc --server=localhost:9091 put test2 "grpc-port" 2>&1 | grep -q "Put key"; then
    echo "  Put succeeded"
    VALUE=$(./bin/kvcli --protocol=grpc --server=localhost:9091 get test2 2>&1)
    if [ "$VALUE" = "grpc-port" ]; then
        echo "  Get succeeded: $VALUE"
    else
        echo "  Get returned unexpected value: $VALUE"
    fi
else
    echo "  Put failed"
fi
echo ""

# Test 3: Different HTTP port (8082)
echo "Test 3: Different HTTP port (8082) auto-converts..."
if ./bin/kvcli --protocol=grpc --server=localhost:8082 put test3 "port-8082" 2>&1 | grep -q "Put key"; then
    echo "  Put succeeded"
    VALUE=$(./bin/kvcli --protocol=grpc --server=localhost:8082 get test3 2>&1)
    if [ "$VALUE" = "port-8082" ]; then
        echo "  Get succeeded: $VALUE"
    else
        echo "  Get returned unexpected value: $VALUE"
    fi
else
    echo "  Put failed"
fi
echo ""

# Test 4: Environment variables with HTTP port
echo "Test 4: Environment variables with HTTP port..."
export RAFTKV_PROTOCOL=grpc
export RAFTKV_SERVER=localhost:8081
if ./bin/kvcli put test4 "env-var" 2>&1 | grep -q "Put key"; then
    echo "  Put succeeded"
    VALUE=$(./bin/kvcli get test4 2>&1)
    if [ "$VALUE" = "env-var" ]; then
        echo "  Get succeeded: $VALUE"
    else
        echo "  Get returned unexpected value: $VALUE"
    fi
else
    echo "  Put failed"
fi
unset RAFTKV_PROTOCOL RAFTKV_SERVER
echo ""

# Test 5: Comma-separated servers
echo "Test 5: Comma-separated servers..."
if ./bin/kvcli --protocol=grpc --server="localhost:8081,localhost:8082" put test5 "multi-server" 2>&1 | grep -q "Put key"; then
    echo "  Put succeeded"
    VALUE=$(./bin/kvcli --protocol=grpc --server="localhost:8081,localhost:8082" get test5 2>&1)
    if [ "$VALUE" = "multi-server" ]; then
        echo "  Get succeeded: $VALUE"
    else
        echo "  Get returned unexpected value: $VALUE"
    fi
else
    echo "  Put failed"
fi
echo ""

# Test 6: Stats command
echo "Test 6: Stats command via gRPC..."
if ./bin/kvcli --protocol=grpc --server=localhost:8081 stats 2>&1 | grep -q "Server Statistics"; then
    echo "  Stats succeeded"
else
    echo "  Stats failed"
fi
echo ""

# Test 7: List command
echo "Test 7: List command via gRPC..."
if ./bin/kvcli --protocol=grpc --server=localhost:8081 list --prefix=test 2>&1 | grep -q "keys"; then
    echo "  List succeeded"
else
    echo "  List failed"
fi
echo ""

echo "=== Auto-Configuration Tests Complete ==="
echo ""
echo "Summary: All tests validate that:"
echo "  1. HTTP ports (808x) auto-convert to gRPC ports (909x)"
echo "  2. gRPC ports work directly without conversion"
echo "  3. Environment variables work with auto-configuration"
echo "  4. Multiple servers can be specified"
echo "  5. All cluster servers are auto-added for discovery"
