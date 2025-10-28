.PHONY: all run build build-cli install-cli test test-coverage bench clean fmt lint vet help test-api load-test run-server

# Variables
BINARY_NAME=kvstore
CLI_NAME=kvcli
GO=go
GOFLAGS=-v

# commands
all: test build

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
	@echo "✓ kvcli installed successfully"

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
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchmem ./internal/storage

fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

clean:
	@echo "Cleaning..."
	rm -rf bin/ coverage.out coverage.html cpu.out mem.out data/

help:
	@echo "Available targets:"
	@echo "  make run             - Run server locally"
	@echo "  make build           - Build server binary"
	@echo "  make build-cli       - Build CLI binary"
	@echo "  make install-cli     - Install CLI"
	@echo "  make test            - Run tests with race detector"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make bench           - Run benchmarks"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make fmt             - Format code"
	@echo "  make run-serve       - Run HTTP server"
	@echo "  make test-api        - Test HTTP API"
	@echo "  make load-test       - Load Test HTTP API"
