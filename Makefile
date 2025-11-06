.PHONY: all run build build-cli test test-storage test-compaction test-integration test-integration-fast test-coverage bench bench-compaction bench-grpc bench-http clean fmt help run-server raft-cluster raft-stop raft-node1 raft-node2 raft-node3 raft-status raft-test-api raft-test-grpc quickstart proto

# Variables
BINARY_NAME=kvstore
CLI_NAME=kvcli
GO=go
GOFLAGS=-v

# commands
all: test build

# Proto targets
proto:
	@echo "Generating protobuf code..."
	@protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		api/proto/kv.proto
	@echo "Protobuf code generated"

run:
	@echo "Starting server..."
	$(GO) run ./cmd/$(BINARY_NAME)

build:
	@echo "Building server..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/$(BINARY_NAME)

run-server: build
	@echo "Starting server..."
	./bin/kvstore \
		--http-addr=:8080 \
		--data-dir=./data \
		--log-level=info \
		--sync-on-write=false \
		--snapshot-every=1000

build-cli:
	@echo "Building CLI..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o bin/$(CLI_NAME) ./cmd/$(CLI_NAME)

test:
	@echo "Running tests..."
	$(GO) test -v -race -cover ./...

test-storage:
	@echo "Running storage tests only..."
	$(GO) test -v -race ./internal/storage

test-compaction:
	@echo "Running compaction tests..."
	$(GO) test -v -race -run="Compaction|Snapshot" ./internal/storage

test-integration:
	@echo "Running integration tests..."
	$(GO) test -v -timeout=300s ./test/integration

test-integration-fast:
	@echo "Running fast integration tests (single test)..."
	$(GO) test -v -timeout=60s -run="TestThreeNodeCluster_BasicOperations" ./test/integration

test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

bench:
	@echo "Running storage benchmarks..."
	$(GO) test -bench=. -benchmem ./internal/storage

bench-compaction:
	@echo "Running compaction benchmarks..."
	$(GO) test -bench=BenchmarkWALCompaction -benchmem ./internal/storage -run=^$

bench-grpc:
	@echo "Running gRPC server benchmarks..."
	$(GO) test ./internal/server -bench=BenchmarkGRPCServer -benchmem -run=^$ -timeout 60s

bench-http:
	@echo "Running HTTP server benchmarks..."
	$(GO) test ./internal/server -bench=BenchmarkHTTPServer -benchmem -run=^$ -timeout 60s

bench-all:
	@echo "Running all benchmarks..."
	@./scripts/benchmark.sh -t all -o benchmarks/results.txt

bench-storage:
	@echo "Running storage benchmarks..."
	@./scripts/benchmark.sh -t storage

bench-server:
	@echo "Running server benchmarks..."
	@./scripts/benchmark.sh -t server

bench-cluster:
	@echo "Running cluster benchmarks..."
	@./scripts/benchmark.sh -t cluster

bench-profile:
	@echo "Running benchmarks with profiling..."
	@./scripts/benchmark.sh -t all -m -p -o benchmarks/results.txt

fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

clean:
	@echo "Cleaning..."
	rm -rf bin/ coverage.out coverage.html cpu.out mem.out data/ test-data/ benchmark_results.txt

# Docker targets
docker-build:
	@echo "Building Docker image..."
	docker build -t raftkv:latest -f deployments/docker/Dockerfile .

docker-up:
	@echo "Starting RaftKV cluster with Docker Compose..."
	docker-compose -f deployments/docker/docker-compose.simple.yml up --build

docker-up-lb:
	@echo "Starting RaftKV cluster with HAProxy load balancer..."
	docker-compose -f deployments/docker/docker-compose.yml up --build

docker-down:
	@echo "Stopping RaftKV cluster..."
	docker-compose -f deployments/docker/docker-compose.simple.yml down

docker-down-lb:
	@echo "Stopping RaftKV cluster with load balancer..."
	docker-compose -f deployments/docker/docker-compose.yml down

docker-clean:
	@echo "Cleaning Docker resources..."
	docker-compose -f deployments/docker/docker-compose.simple.yml down -v
	docker-compose -f deployments/docker/docker-compose.yml down -v

docker-logs:
	@echo "Tailing cluster logs..."
	docker-compose -f deployments/docker/docker-compose.simple.yml logs -f

docker-test:
	@echo "Testing Docker cluster..."
	@sleep 3
	@echo "\n1. Writing data to node1..."
	@docker exec raftkv-node1 kvcli --server=localhost:8080 put test:key1 "Hello from Docker"
	@echo "\n2. Reading from node2..."
	@docker exec raftkv-node2 kvcli --server=localhost:8080 get test:key1
	@echo "\n3. Checking cluster status..."
	@curl -s http://localhost:8081/cluster/nodes | jq
	@echo "\n4. Getting cluster leader..."
	@curl -s http://localhost:8081/cluster/leader | jq

# Raft cluster targets
raft-cluster: build
	@echo "Starting 3-node Raft cluster..."
	@./scripts/start-cluster.sh

raft-stop:
	@echo "Stopping Raft cluster..."
	@./scripts/stop-cluster.sh

raft-node1: build
	@echo "Starting node1 (bootstrap) with gRPC..."
	@mkdir -p data/node1/raft logs
	./bin/kvstore \
		--raft \
		--grpc \
		--node-id=node1 \
		--http-addr=:8081 \
		--grpc-addr=:9091 \
		--raft-addr=127.0.0.1:7001 \
		--raft-dir=./data/node1/raft \
		--bootstrap \
		--log-level=info

raft-node2: build
	@echo "Starting node2 with gRPC..."
	@mkdir -p data/node2/raft logs
	./bin/kvstore \
		--raft \
		--grpc \
		--node-id=node2 \
		--http-addr=:8082 \
		--grpc-addr=:9092 \
		--raft-addr=127.0.0.1:7002 \
		--raft-dir=./data/node2/raft \
		--log-level=info

raft-node3: build
	@echo "Starting node3 with gRPC..."
	@mkdir -p data/node3/raft logs
	./bin/kvstore \
		--raft \
		--grpc \
		--node-id=node3 \
		--http-addr=:8083 \
		--grpc-addr=:9093 \
		--raft-addr=127.0.0.1:7003 \
		--raft-dir=./data/node3/raft \
		--log-level=info

raft-status:
	@echo "Checking cluster status..."
	@echo "\n1. Cluster nodes:"
	@curl -s http://localhost:8081/cluster/nodes | jq
	@echo "\n2. Current leader:"
	@curl -s http://localhost:8081/cluster/leader | jq
	@echo "\n3. Node1 health:"
	@curl -s http://localhost:8081/health | jq
	@echo "\n4. Node2 health:"
	@curl -s http://localhost:8082/health | jq
	@echo "\n5. Node3 health:"
	@curl -s http://localhost:8083/health | jq

raft-test-api:
	@echo "Testing Raft cluster API..."
	@echo "\n1. Writing to node1 (may redirect):"
	@curl -s -X PUT -d "Alice" http://localhost:8081/keys/user:1 && echo ""
	@echo "\n2. Writing to node2 (will redirect to leader):"
	@curl -s -X PUT -d "Bob" http://localhost:8082/keys/user:2 && echo ""
	@echo "\n3. Reading from node1:"
	@curl -s http://localhost:8081/keys/user:1
	@echo "\n4. Reading from node2:"
	@curl -s http://localhost:8082/keys/user:2
	@echo "\n5. Reading from node3:"
	@curl -s http://localhost:8083/keys/user:1
	@echo "\n6. List keys from any node:"
	@curl -s "http://localhost:8081/keys?prefix=user" | jq
	@echo "\n7. Cluster stats from leader:"
	@curl -s http://localhost:8081/stats | jq

raft-test-grpc: build-cli
	@echo "Testing Raft cluster via gRPC..."
	@echo "\n=== Testing with HTTP port (auto-converts to gRPC) ==="
	@echo "1. Put via gRPC (using HTTP port 8081):"
	@./bin/kvcli --protocol=grpc --server=localhost:8081 put user:grpc:1 "Alice"
	@echo "\n2. Get via gRPC:"
	@./bin/kvcli --protocol=grpc --server=localhost:8081 get user:grpc:1
	@echo "\n3. Put to different node (auto-discovery):"
	@./bin/kvcli --protocol=grpc --server=localhost:8082 put user:grpc:2 "Bob"
	@echo "\n4. List via gRPC:"
	@./bin/kvcli --protocol=grpc --server=localhost:8081 list --prefix=user:grpc
	@echo "\n5. Stats via gRPC:"
	@./bin/kvcli --protocol=grpc --server=localhost:8081 stats
	@echo "\n=== Testing with gRPC port directly ==="
	@echo "6. Put via gRPC (using gRPC port 9091):"
	@./bin/kvcli --protocol=grpc --server=localhost:9091 put user:grpc:3 "Charlie"
	@echo "\n7. Get via gRPC:"
	@./bin/kvcli --protocol=grpc --server=localhost:9091 get user:grpc:3

quickstart: build build-cli
	@echo "🚀 Starting RaftKV quickstart..."
	@echo ""
	@$(MAKE) raft-cluster
	@echo ""
	@echo "Waiting for cluster to be ready (5 seconds)..."
	@sleep 5
	@echo ""
	@echo "=== Testing cluster with CLI (gRPC) ==="
	@./bin/kvcli --protocol=grpc --server=localhost:8081 put demo:name "RaftKV"
	@echo ""
	@./bin/kvcli --protocol=grpc --server=localhost:8081 put demo:version "1.0"
	@echo ""
	@echo "Getting values back..."
	@./bin/kvcli --protocol=grpc --server=localhost:8081 get demo:name
	@./bin/kvcli --protocol=grpc --server=localhost:8081 get demo:version
	@echo ""
	@echo "Cluster statistics:"
	@./bin/kvcli --protocol=grpc --server=localhost:8081 stats
	@echo ""
	@echo "RaftKV is ready!"
	@echo ""
	@echo "Cluster nodes:"
	@echo "  Node1: http://localhost:8081 (HTTP), localhost:9091 (gRPC)"
	@echo "  Node2: http://localhost:8082 (HTTP), localhost:9092 (gRPC)"
	@echo "  Node3: http://localhost:8083 (HTTP), localhost:9093 (gRPC)"
	@echo ""
	@echo "Try commands:"
	@echo "  ./bin/kvcli --protocol=grpc --server=localhost:8081 put key value"
	@echo "  ./bin/kvcli --protocol=grpc --server=localhost:8081 get key"
	@echo ""
	@echo "To stop: make raft-stop"

help:
	@echo "Available targets:"
	@echo ""
	@echo "Quick Start:"
	@echo "  make quickstart      - Build, start cluster, and test (one command!)"
	@echo ""
	@echo "Single-node mode:"
	@echo "  make run             - Run server locally (single-node)"
	@echo "  make run-server      - Run HTTP server with default config"
	@echo ""
	@echo "Raft cluster mode:"
	@echo "  make raft-cluster    - Start 3-node Raft cluster (automated)"
	@echo "  make raft-stop       - Stop Raft cluster"
	@echo "  make raft-node1      - Start node1 manually (bootstrap)"
	@echo "  make raft-node2      - Start node2 manually"
	@echo "  make raft-node3      - Start node3 manually"
	@echo "  make raft-status     - Check cluster status"
	@echo "  make raft-test-api   - Test Raft cluster via HTTP API"
	@echo "  make raft-test-grpc  - Test Raft cluster via gRPC"
	@echo ""
	@echo "Build & test:"
	@echo "  make proto           - Generate protobuf code from .proto files"
	@echo "  make build           - Build server binary"
	@echo "  make build-cli       - Build CLI binary"
	@echo "  make test            - Run all tests with race detector"
	@echo "  make test-storage    - Run storage tests only"
	@echo "  make test-compaction - Run compaction and snapshot tests"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make bench           - Run storage benchmarks"
	@echo "  make bench-compaction - Run compaction benchmarks"
	@echo "  make bench-grpc      - Run gRPC server benchmarks"
	@echo "  make bench-http      - Run HTTP server benchmarks"
	@echo ""
	@echo "Docker deployment:"
	@echo "  make docker-build    - Build Docker image"
	@echo "  make docker-up       - Start 3-node cluster with Docker Compose"
	@echo "  make docker-up-lb    - Start cluster with HAProxy load balancer"
	@echo "  make docker-down     - Stop Docker cluster"
	@echo "  make docker-clean    - Stop and remove all Docker resources"
	@echo "  make docker-logs     - View cluster logs"
	@echo "  make docker-test     - Test the Docker cluster"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean           - Clean build artifacts and data"
	@echo "  make fmt             - Format code"
	@echo ""
	@echo "Examples:"
	@echo "  # Quickstart (recommended)"
	@echo "  make quickstart"
	@echo ""
	@echo "  # Development workflow"
	@echo "  make test-storage && make build"
	@echo ""
	@echo "  # Start cluster and test"
	@echo "  make raft-cluster && sleep 5 && make raft-test-grpc"
	@echo ""
	@echo "  # Manual 3-node setup (in separate terminals)"
	@echo "  make raft-node1  # Terminal 1"
	@echo "  make raft-node2  # Terminal 2"
	@echo "  make raft-node3  # Terminal 3"
	@echo ""
	@echo "  # Use CLI with cluster"
	@echo "  ./bin/kvcli --protocol=grpc --server=localhost:8081 put key value"
	@echo "  ./bin/kvcli --protocol=grpc --server=localhost:8081 get key"
