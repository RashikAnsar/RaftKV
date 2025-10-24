.PHONY build

# Variables
BINARY_NAME=kvstore
CLI_NAME=kvcli
GO=go
GOFLAGS=-v

# commands
run:
	@echo "Starting server..."
	$(GO) run ./cmd/kvstore


build:
	@echo "Building server..."
	@mkdir -p bin
	$(GO) build $(GOFLAGS) -o bin/$(BINARY_NAME) ./cmd/kvstore

help:
	@echo "Available targets:"
	@echo "make run		- Run server locally"
	@echo "make build		- Build binaries"