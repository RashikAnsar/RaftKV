.PHONY: all run build build-cli test test-storage test-compaction test-integration test-integration-fast test-coverage bench bench-compaction bench-grpc bench-http clean fmt help run-server raft-cluster raft-stop raft-node1 raft-node2 raft-node3 raft-status raft-test-api raft-test-grpc quickstart proto tls-certs tls-server tls-cluster tls-test

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
	@echo "Running all benchmarks..."
	@./scripts/benchmark.sh -t all

bench-storage:
	@echo "Running storage benchmarks..."
	@./scripts/benchmark.sh -t storage

bench-server:
	@echo "Running server benchmarks..."
	@./scripts/benchmark.sh -t server

bench-batch:
	@echo "Running batch write benchmarks..."
	@./scripts/benchmark.sh -t batch

bench-cluster:
	@echo "Note: Cluster benchmarks are skipped (.skip files)"
	@echo "See test/benchmark/CLUSTER_BENCHMARKS.md for details"
	@./scripts/benchmark.sh -t cluster

bench-results:
	@echo "Running all benchmarks with results file..."
	@./scripts/benchmark.sh -t all -o benchmarks/results.txt

bench-profile:
	@echo "Running benchmarks with profiling..."
	@./scripts/benchmark.sh -t all -m -p -o benchmarks/results.txt

bench-compare:
	@echo "Running benchmarks for comparison (5 iterations)..."
	@./scripts/benchmark.sh -t all -c 5 -o benchmarks/baseline.txt

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

# Monitoring targets
docker-up-monitoring:
	@echo "Starting RaftKV cluster with monitoring stack..."
	@echo "This will start: 3 RaftKV nodes + HAProxy + Prometheus + Grafana"
	@docker-compose -f deployments/docker/docker-compose.yml \
		-f deployments/docker/docker-compose.monitoring.yml up --build

docker-down-monitoring:
	@echo "Stopping RaftKV cluster and monitoring stack..."
	@docker-compose -f deployments/docker/docker-compose.yml \
		-f deployments/docker/docker-compose.monitoring.yml down

docker-monitoring-only:
	@echo "Starting monitoring stack only (Prometheus + Grafana)..."
	@echo "Note: RaftKV cluster must be running first"
	@docker-compose -f deployments/docker/docker-compose.monitoring.yml up

docker-monitoring-stop:
	@echo "Stopping monitoring stack..."
	@docker-compose -f deployments/docker/docker-compose.monitoring.yml down

docker-monitoring-clean:
	@echo "Cleaning monitoring data volumes..."
	@docker-compose -f deployments/docker/docker-compose.monitoring.yml down -v

docker-monitoring-logs:
	@echo "Tailing monitoring logs..."
	@docker-compose -f deployments/docker/docker-compose.monitoring.yml logs -f

docker-monitoring-urls:
	@echo ""
	@echo "📊 Monitoring Stack URLs:"
	@echo "  Prometheus: http://localhost:9999"
	@echo "  Grafana:    http://localhost:3000 (admin/admin)"
	@echo ""
	@echo "RaftKV Cluster URLs:"
	@echo "  HAProxy:    http://localhost:8080"
	@echo "  Node1:      http://localhost:8081"
	@echo "  Node2:      http://localhost:8082"
	@echo "  Node3:      http://localhost:8083"
	@echo ""
	@echo "Dashboards available in Grafana:"
	@echo "  - RaftKV Overview (operations, latency, errors)"
	@echo "  - Storage & Persistence (WAL, snapshots, cache)"
	@echo "  - Cluster Health (Raft metrics)"
	@echo ""

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

# TLS/Security targets
tls-certs:
	@echo "🔐 Generating TLS certificates..."
	@./scripts/generate-certs.sh
	@echo "Certificates generated in certs/ directory"

tls-server: build tls-certs
	@echo "Starting single-node server with TLS..."
	@mkdir -p data
	./bin/kvstore \
		--http-addr=:8443 \
		--data-dir=./data \
		--tls \
		--tls-cert=certs/server-cert.pem \
		--tls-key=certs/server-key.pem \
		--tls-ca=certs/ca-cert.pem \
		--log-level=info

tls-server-mtls: build tls-certs
	@echo "Starting single-node server with mTLS (mutual TLS)..."
	@mkdir -p data
	./bin/kvstore \
		--http-addr=:8443 \
		--data-dir=./data \
		--tls \
		--mtls \
		--tls-cert=certs/server-cert.pem \
		--tls-key=certs/server-key.pem \
		--tls-ca=certs/ca-cert.pem \
		--log-level=info

tls-cluster: build tls-certs
	@echo "Starting 3-node Raft cluster with TLS..."
	@mkdir -p data/node1 data/node2 data/node3 logs
	@echo "Starting node1 (bootstrap)..."
	@./bin/kvstore \
		--raft --bootstrap \
		--node-id=node1 \
		--raft-addr=localhost:7001 \
		--http-addr=:8081 \
		--data-dir=./data/node1 \
		--tls \
		--tls-cert=certs/node1-cert.pem \
		--tls-key=certs/node1-key.pem \
		--tls-ca=certs/ca-cert.pem \
		--log-level=info > logs/node1.log 2>&1 &
	@echo "Node1 PID: $$!" > logs/node1.pid
	@sleep 3
	@echo "Starting node2 (join)..."
	@./bin/kvstore \
		--raft \
		--node-id=node2 \
		--raft-addr=localhost:7002 \
		--http-addr=:8082 \
		--data-dir=./data/node2 \
		--join=https://localhost:8081 \
		--tls \
		--tls-cert=certs/node2-cert.pem \
		--tls-key=certs/node2-key.pem \
		--tls-ca=certs/ca-cert.pem \
		--log-level=info > logs/node2.log 2>&1 &
	@echo "Node2 PID: $$!" > logs/node2.pid
	@sleep 3
	@echo "Starting node3 (join)..."
	@./bin/kvstore \
		--raft \
		--node-id=node3 \
		--raft-addr=localhost:7003 \
		--http-addr=:8083 \
		--data-dir=./data/node3 \
		--join=https://localhost:8081 \
		--tls \
		--tls-cert=certs/node3-cert.pem \
		--tls-key=certs/node3-key.pem \
		--tls-ca=certs/ca-cert.pem \
		--log-level=info > logs/node3.log 2>&1 &
	@echo "Node3 PID: $$!" > logs/node3.pid
	@sleep 2
	@echo ""
	@echo "TLS cluster started!"
	@echo ""
	@echo "Cluster nodes (HTTPS):"
	@echo "  Node1: https://localhost:8081"
	@echo "  Node2: https://localhost:8082"
	@echo "  Node3: https://localhost:8083"
	@echo ""
	@echo "Logs:"
	@echo "  Node1: logs/node1.log"
	@echo "  Node2: logs/node2.log"
	@echo "  Node3: logs/node3.log"
	@echo ""
	@echo "Test with:"
	@echo "  make tls-test"
	@echo ""
	@echo "Stop with:"
	@echo "  make raft-stop"

tls-test:
	@echo "🔐 Testing TLS cluster..."
	@echo ""
	@echo "1. Health check (node1):"
	@curl -s --cacert certs/ca-cert.pem https://localhost:8081/health | jq
	@echo ""
	@echo "2. Write data (HTTPS):"
	@curl -s --cacert certs/ca-cert.pem -X PUT -d "Hello TLS World" https://localhost:8081/keys/test:tls
	@echo ""
	@echo "3. Read data (HTTPS):"
	@curl -s --cacert certs/ca-cert.pem https://localhost:8081/keys/test:tls
	@echo ""
	@echo "4. Read from node2 (replication check):"
	@curl -s --cacert certs/ca-cert.pem https://localhost:8082/keys/test:tls
	@echo ""
	@echo "5. Cluster status:"
	@curl -s --cacert certs/ca-cert.pem https://localhost:8081/cluster/nodes | jq
	@echo ""
	@echo "TLS cluster is working!"

tls-test-mtls:
	@echo "🔐 Testing mTLS (mutual TLS) authentication..."
	@echo ""
	@echo "1. Request without client cert (should fail):"
	@curl -s --cacert certs/ca-cert.pem https://localhost:8443/health || echo "❌ Rejected (expected)"
	@echo ""
	@echo "2. Request with client cert (should succeed):"
	@curl -s --cacert certs/ca-cert.pem \
		--cert certs/client-cert.pem \
		--key certs/client-key.pem \
		https://localhost:8443/health | jq
	@echo ""
	@echo "mTLS authentication working!"

help:
	@echo "Available targets:"
	@echo ""
	@echo "Quick Start:"
	@echo "  make quickstart      - Build, start cluster, and test (one command!)"
	@echo ""
	@echo "TLS/Security (NEW!):"
	@echo "  make tls-certs       - Generate TLS certificates"
	@echo "  make tls-server      - Start single-node server with TLS"
	@echo "  make tls-server-mtls - Start server with mutual TLS (client auth)"
	@echo "  make tls-cluster     - Start 3-node Raft cluster with TLS"
	@echo "  make tls-test        - Test TLS cluster with HTTPS requests"
	@echo "  make tls-test-mtls   - Test mutual TLS authentication"
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
	@echo "  make test-integration - Run integration tests"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make bench           - Run all benchmarks"
	@echo "  make bench-storage   - Run storage benchmarks"
	@echo "  make bench-server    - Run HTTP server benchmarks"
	@echo "  make bench-batch     - Run batch write benchmarks"
	@echo "  make bench-cluster   - Info about cluster benchmarks (skipped)"
	@echo "  make bench-results   - Run all benchmarks, save to file"
	@echo "  make bench-profile   - Run benchmarks with CPU/memory profiling"
	@echo "  make bench-compare   - Run benchmarks 5x for comparison baseline"
	@echo ""
	@echo "Docker deployment:"
	@echo "  make docker-build           - Build Docker image"
	@echo "  make docker-up              - Start 3-node cluster with Docker Compose"
	@echo "  make docker-up-lb           - Start cluster with HAProxy load balancer"
	@echo "  make docker-up-monitoring   - Start cluster + monitoring (Prometheus + Grafana)"
	@echo "  make docker-down            - Stop Docker cluster"
	@echo "  make docker-down-monitoring - Stop cluster and monitoring"
	@echo "  make docker-clean           - Stop and remove all Docker resources"
	@echo "  make docker-logs            - View cluster logs"
	@echo "  make docker-test            - Test the Docker cluster"
	@echo ""
	@echo "Monitoring (Docker):"
	@echo "  make docker-up-monitoring     - Start RaftKV + Prometheus + Grafana"
	@echo "  make docker-monitoring-only   - Start monitoring stack only"
	@echo "  make docker-monitoring-stop   - Stop monitoring stack"
	@echo "  make docker-monitoring-clean  - Clean monitoring data volumes"
	@echo "  make docker-monitoring-logs   - View monitoring logs"
	@echo "  make docker-monitoring-urls   - Show monitoring URLs"
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
