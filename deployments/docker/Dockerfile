# Multi-stage build for RaftKV
# Stage 1: Build the application
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make

# Set working directory
WORKDIR /app

# Set GOTOOLCHAIN to auto to allow downloading the required Go version
ENV GOTOOLCHAIN=auto

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the server and CLI binaries
RUN make build build-cli

# Stage 2: Create minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 raftkv && \
    adduser -D -u 1000 -G raftkv raftkv

# Create data directory
RUN mkdir -p /data && chown raftkv:raftkv /data

# Copy binaries from builder
COPY --from=builder /app/bin/kvstore /usr/local/bin/kvstore
COPY --from=builder /app/bin/kvcli /usr/local/bin/kvcli

# Switch to non-root user
USER raftkv

# Set working directory
WORKDIR /data

# Expose ports
# 8080: HTTP API
# 9090: gRPC API
# 7000: Raft communication
EXPOSE 8080 9090 7000

# Health check - use the /health endpoint instead of trying to get a key
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["/usr/local/bin/kvstore"]
CMD ["--http-addr=:8080", "--grpc-addr=:9090", "--data-dir=/data"]
