#!/bin/bash
# Script to start a 3-node Raft cluster locally

set -e

# Build the binary
echo "Building kvstore binary..."
go build -o bin/kvstore ./cmd/kvstore

# Clean up old data
echo "Cleaning up old data..."
rm -rf data/node1 data/node2 data/node3

# Start node 1 (bootstrap)
echo "Starting node1 (bootstrap)..."
./bin/kvstore \
  --raft \
  --grpc \
  --node-id=node1 \
  --http-addr=:8081 \
  --grpc-addr=:9091 \
  --raft-addr=127.0.0.1:7001 \
  --raft-dir=./data/node1/raft \
  --bootstrap \
  --log-level=info \
  > logs/node1.log 2>&1 &

NODE1_PID=$!
echo "Node1 started with PID $NODE1_PID"

# Wait for node1 to be ready
echo "Waiting for node1 to be ready..."
sleep 3

# Start node 2
echo "Starting node2..."
./bin/kvstore \
  --raft \
  --grpc \
  --node-id=node2 \
  --http-addr=:8082 \
  --grpc-addr=:9092 \
  --raft-addr=127.0.0.1:7002 \
  --raft-dir=./data/node2/raft \
  --log-level=info \
  > logs/node2.log 2>&1 &

NODE2_PID=$!
echo "Node2 started with PID $NODE2_PID"

# Start node 3
echo "Starting node3..."
./bin/kvstore \
  --raft \
  --grpc \
  --node-id=node3 \
  --http-addr=:8083 \
  --grpc-addr=:9093 \
  --raft-addr=127.0.0.1:7003 \
  --raft-dir=./data/node3/raft \
  --log-level=info \
  > logs/node3.log 2>&1 &

NODE3_PID=$!
echo "Node3 started with PID $NODE3_PID"

# Wait for nodes to be ready
echo "Waiting for all nodes to be ready..."
sleep 3

# Join node2 and node3 to the cluster
echo "Adding node2 to cluster..."
curl -X POST http://localhost:8081/cluster/join \
  -H "Content-Type: application/json" \
  -d '{"node_id":"node2","addr":"127.0.0.1:7002"}' \
  && echo ""

echo "Adding node3 to cluster..."
curl -X POST http://localhost:8081/cluster/join \
  -H "Content-Type: application/json" \
  -d '{"node_id":"node3","addr":"127.0.0.1:7003"}' \
  && echo ""

echo ""
echo "✅ 3-node cluster started successfully!"
echo ""
echo "Node endpoints:"
echo "  Node1: http://localhost:8081"
echo "  Node2: http://localhost:8082"
echo "  Node3: http://localhost:8083"
echo ""
echo "Check cluster status:"
echo "  curl http://localhost:8081/cluster/nodes | jq"
echo ""
echo "Check cluster leader:"
echo "  curl http://localhost:8081/cluster/leader | jq"
echo ""
echo "To stop the cluster:"
echo "  kill $NODE1_PID $NODE2_PID $NODE3_PID"
echo ""
echo "PIDs saved to: logs/cluster.pids"
echo "$NODE1_PID $NODE2_PID $NODE3_PID" > logs/cluster.pids
