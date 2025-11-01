.PHONY: all run build build-cli install-cli test test-coverage bench bench-grpc bench-http bench-compare clean fmt lint vet help test-api load-test run-server raft-cluster raft-stop raft-node1 raft-node2 raft-node3 raft-status raft-test-api proto

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

# NOTE: before running this run-server
test-api:
	@echo "Testing API..."
	@echo "\n1. Health check:"
	@curl -s http://localhost:8080/health | jq
	@echo "\n2. Put key:"
	@curl -s -X PUT -d "Hello, World!" http://localhost:8080/keys/greeting
	@echo "\n3. Get key:"
	@curl -s http://localhost:8080/keys/greeting
	@echo "\n4. List keys:"
	@curl -s http://localhost:8080/keys | jq
	@echo "\n5. Stats:"
	@curl -s http://localhost:8080/stats | jq

# NOTE: before running this run-server
load-test:
	@echo "Running load test..."
	@echo "PUT 1000 keys..."
	@for i in $$(seq 1 1000); do \
		curl -s -X PUT -d "value-$$i" http://localhost:8080/keys/key-$$i > /dev/null & \
	done
	@wait
	@echo "Done!"
	@curl -s http://localhost:8080/stats | jq

install-cli:
	@echo "Installing kvcli to /usr/local/bin..."
	@sudo cp bin/kvcli /usr/local/bin/
	@echo "kvcli installed successfully"

build-cli:
	@echo "Building CLI..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o bin/$(CLI_NAME) ./cmd/$(CLI_NAME)

test:
	@echo "Running tests..."
	$(GO) test -v -race -cover ./...

test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

bench:
	@echo "Running storage benchmarks..."
	$(GO) test -bench=. -benchmem ./internal/storage

bench-grpc:
	@echo "Running gRPC server benchmarks..."
	$(GO) test ./internal/server -bench=BenchmarkGRPCServer -benchmem -run=^$ -timeout 60s

bench-http:
	@echo "Running HTTP server benchmarks..."
	$(GO) test ./internal/server -bench=BenchmarkHTTPServer -benchmem -run=^$ -timeout 60s

bench-compare:
	@echo "Running benchmark comparison (gRPC vs HTTP)..."
	@echo "=== gRPC Benchmarks ===" > benchmark_results.txt
	@$(GO) test ./internal/server -bench=BenchmarkGRPCServer -benchmem -run=^$ 2>&1 | grep -E "^(Benchmark|goos|goarch|cpu)" >> benchmark_results.txt
	@echo "" >> benchmark_results.txt
	@echo "=== HTTP Benchmarks ===" >> benchmark_results.txt
	@$(GO) test ./internal/server -bench=BenchmarkHTTPServer -benchmem -run=^$ 2>&1 | grep -E "^(Benchmark|goos|goarch|cpu)" >> benchmark_results.txt
	@echo ""
	@cat benchmark_results.txt
	@echo ""
	@echo "Results saved to benchmark_results.txt"
	@echo "See BENCHMARK_COMPARISON.md for detailed analysis"

fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

clean:
	@echo "Cleaning..."
	rm -rf bin/ coverage.out coverage.html cpu.out mem.out data/

# Raft cluster targets
raft-cluster: build
	@echo "Starting 3-node Raft cluster..."
	@./scripts/start-cluster.sh

raft-stop:
	@echo "Stopping Raft cluster..."
	@./scripts/stop-cluster.sh

raft-node1: build
	@echo "Starting node1 (bootstrap)..."
	@mkdir -p data/node1/raft logs
	./bin/kvstore \
		--raft \
		--node-id=node1 \
		--http-addr=:8081 \
		--raft-addr=127.0.0.1:7001 \
		--raft-dir=./data/node1/raft \
		--bootstrap \
		--log-level=info

raft-node2: build
	@echo "Starting node2..."
	@mkdir -p data/node2/raft logs
	./bin/kvstore \
		--raft \
		--node-id=node2 \
		--http-addr=:8082 \
		--raft-addr=127.0.0.1:7002 \
		--raft-dir=./data/node2/raft \
		--log-level=info

raft-node3: build
	@echo "Starting node3..."
	@mkdir -p data/node3/raft logs
	./bin/kvstore \
		--raft \
		--node-id=node3 \
		--http-addr=:8083 \
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

help:
	@echo "Available targets:"
	@echo ""
	@echo "Single-node mode:"
	@echo "  make run             - Run server locally (single-node)"
	@echo "  make run-server      - Run HTTP server (single-node)"
	@echo "  make test-api        - Test HTTP API (single-node)"
	@echo "  make load-test       - Load test HTTP API (single-node)"
	@echo ""
	@echo "Raft cluster mode:"
	@echo "  make raft-cluster    - Start 3-node Raft cluster (automated)"
	@echo "  make raft-stop       - Stop Raft cluster"
	@echo "  make raft-node1      - Start node1 manually (bootstrap)"
	@echo "  make raft-node2      - Start node2 manually"
	@echo "  make raft-node3      - Start node3 manually"
	@echo "  make raft-status     - Check cluster status"
	@echo "  make raft-test-api   - Test Raft cluster API"
	@echo ""
	@echo "Build & test:"
	@echo "  make proto           - Generate protobuf code from .proto files"
	@echo "  make build           - Build server binary"
	@echo "  make build-cli       - Build CLI binary"
	@echo "  make install-cli     - Install CLI to /usr/local/bin"
	@echo "  make test            - Run tests with race detector"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make bench           - Run storage benchmarks"
	@echo "  make bench-grpc      - Run gRPC server benchmarks"
	@echo "  make bench-http      - Run HTTP server benchmarks"
	@echo "  make bench-compare   - Compare gRPC vs HTTP performance"
	@echo ""
	@echo "Utilities:"
	@echo "  make clean           - Clean build artifacts and data"
	@echo "  make fmt             - Format code"
	@echo ""
	@echo "Examples:"
	@echo "  # Start cluster and test"
	@echo "  make raft-cluster && sleep 5 && make raft-test-api"
	@echo ""
	@echo "  # Manual 3-node setup (in separate terminals)"
	@echo "  make raft-node1  # Terminal 1"
	@echo "  make raft-node2  # Terminal 2"
	@echo "  make raft-node3  # Terminal 3"
