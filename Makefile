.PHONY: all run build build-cli test test-coverage bench clean fmt lint vet help

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
	@echo "  make test            - Run tests with race detector"
	@echo "  make test-coverage   - Run tests with coverage report"
	@echo "  make bench           - Run benchmarks"
	@echo "  make clean           - Clean build artifacts"
	@echo "  make fmt             - Format code"
