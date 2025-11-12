# Contributing to RaftKV

> Thank you for considering contributing to RaftKV! This guide will help you get started.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Coding Guidelines](#coding-guidelines)
- [Testing Guidelines](#testing-guidelines)
- [Pull Request Process](#pull-request-process)
- [Release Process](#release-process)

---

## Code of Conduct

### Our Pledge

We are committed to providing a welcoming and inclusive environment for all contributors, regardless of background or identity.

### Expected Behavior

- Use welcoming and inclusive language
- Be respectful of differing viewpoints
- Accept constructive criticism gracefully
- Focus on what is best for the community
- Show empathy towards other contributors

### Unacceptable Behavior

- Harassment, discrimination, or offensive comments
- Trolling, insulting, or derogatory language
- Public or private harassment
- Publishing others' private information

### Enforcement

Violations may result in temporary or permanent ban from the project. Report violations to the project maintainers.

---

## Getting Started

### Prerequisites

**Required:**
- **Go 1.21+** ([Download](https://golang.org/dl/))
- **Git** ([Download](https://git-scm.com/downloads))
- **Make** (usually pre-installed on Linux/macOS)

**Recommended:**
- **Docker** ([Download](https://www.docker.com/get-started)) - for integration tests
- **Visual Studio Code** with Go extension
- **golangci-lint** ([Install](https://golangci-lint.run/usage/install/))

**Verify Installation:**

```bash
go version        # Should be 1.21+
git --version
make --version
docker --version
```

### Fork and Clone

```bash
# 1. Fork the repository on GitHub
# Click "Fork" button at https://github.com/RashikAnsar/raftkv

# 2. Clone your fork
git clone https://github.com/YOUR_USERNAME/raftkv.git
cd raftkv

# 3. Add upstream remote
git remote add upstream https://github.com/YOUR_USERNAME/raftkv.git

# 4. Verify remotes
git remote -v
```

---

## Development Setup

### Build from Source

```bash
# Install dependencies
go mod download

# Build server and CLI
make build

# Verify build
./bin/kvstore --version
./bin/kvcli --version
```

### Run Tests

```bash
# Run all tests
make test

# Run with race detector
make test-race

# Run with coverage
make test-coverage

# View coverage report
go tool cover -html=coverage.out
```

### Run Locally

**Single-Node Mode:**

```bash
# Run with default settings
make run

# Run with custom config
./bin/kvstore --config config/config.dev.yaml

# Run with flags
./bin/kvstore \
  --http-addr :8080 \
  --data-dir ./data \
  --log-level debug
```

**3-Node Cluster (Docker Compose):**

```bash
# Start cluster
make docker-up

# View logs
make docker-logs

# Stop cluster
make docker-down
```

### Development Tools

**Code Formatting:**

```bash
# Format code
gofmt -w .

# Or use make
make fmt
```

**Linting:**

```bash
# Install golangci-lint
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Run linter
make lint

# Fix auto-fixable issues
golangci-lint run --fix
```

**Generate Code:**

```bash
# Generate protobuf code
make proto

# Generate mocks (if using mockery)
make mocks
```

---

## Project Structure

### Directory Layout

```
raftkv/
├── cmd/                      # Application entry points
│   ├── kvstore/              # Server binary
│   │   └── main.go          # (349 lines) - Entry point
│   └── kvcli/               # CLI client
│       └── main.go
│
├── internal/                 # Private application code
│   ├── storage/              # Storage engine (5,200 lines total)
│   │   ├── store.go         # (38 lines) - Interface
│   │   ├── memory_store.go  # In-memory B-tree
│   │   ├── durable_store.go # (506 lines) - WAL + snapshot coordinator
│   │   ├── wal.go           # (658 lines) - Write-ahead log
│   │   ├── wal_batch.go     # Batched writes
│   │   ├── snapshot.go      # GOB snapshots
│   │   ├── cache.go         # LRU cache
│   │   └── cached_store.go  # Cache wrapper
│   │
│   ├── consensus/            # Raft consensus
│   │   ├── raft.go          # (461 lines) - Raft wrapper
│   │   └── fsm.go           # (184 lines) - State machine
│   │
│   ├── server/               # API servers
│   │   ├── http.go          # (416 lines) - HTTP server
│   │   ├── raft_http.go     # Raft-aware HTTP
│   │   ├── grpc.go          # gRPC server
│   │   ├── auth_handlers.go # Auth endpoints
│   │   └── middleware.go    # Middleware pipeline
│   │
│   ├── auth/                 # Authentication & authorization
│   │   ├── user.go          # User management
│   │   ├── apikey.go        # API key handling
│   │   ├── jwt.go           # JWT tokens
│   │   ├── middleware.go    # (161 lines) - Auth middleware
│   │   └── types.go         # RBAC roles
│   │
│   ├── security/             # TLS/mTLS
│   │   └── tls.go           # Certificate management
│   │
│   ├── config/               # Configuration
│   │   └── config.go        # (387 lines) - Multi-source config
│   │
│   └── observability/        # Monitoring & logging
│       ├── logger.go         # Structured logging (Zap)
│       └── metrics.go        # (198 lines) - Prometheus metrics
│
├── pkg/                      # Public libraries
│   └── client/               # Client SDK
│       ├── http_client.go
│       └── grpc_client.go
│
├── api/                      # API definitions
│   └── proto/                # Protobuf definitions
│       └── kv.proto
│
├── test/                     # Test suites
│   ├── integration/          # Integration tests (5 files)
│   │   ├── raft_cluster_test.go
│   │   ├── raft_failover_test.go
│   │   ├── raft_consistency_test.go
│   │   ├── raft_membership_test.go
│   │   └── auth_test.go
│   └── benchmark/            # Performance benchmarks
│
├── deployments/              # Deployment configurations
│   ├── docker/               # Docker & Docker Compose
│   ├── monitoring/           # Prometheus & Grafana
│   └── kubernetes/           # K8s manifests (future)
│
├── config/                   # Configuration examples
│   ├── config.example.yaml
│   ├── config.dev.yaml
│   └── config.prod.yaml
│
├── docs/                     # Documentation
│   ├── API_REFERENCE.md
│   ├── DEPLOYMENT.md
│   ├── OPERATIONS.md
│   └── CONTRIBUTING.md (this file)
│
├── scripts/                  # Operational scripts
│   ├── benchmark.sh
│   └── cluster.sh
│
├── go.mod                    # Go module definition
├── go.sum                    # Dependency checksums
├── Makefile                  # Build automation
├── docs/
│   └── ARCHITECTURE.md       # System architecture
└── README.md                 # Project overview
```

### Key Packages

| Package | Purpose | Entry Point |
|---------|---------|-------------|
| `cmd/kvstore` | Server binary | `main.go` |
| `internal/storage` | Storage engine | `store.go` (interface) |
| `internal/consensus` | Raft integration | `raft.go` |
| `internal/server` | HTTP/gRPC servers | `http.go`, `grpc.go` |
| `internal/auth` | Authentication | `middleware.go` |
| `internal/observability` | Metrics & logging | `metrics.go`, `logger.go` |
| `pkg/client` | Client SDK | `http_client.go` |

---

## Coding Guidelines

### Go Style Guide

Follow the **official Go style guidelines:**

- [Effective Go](https://golang.org/doc/effective_go)
- [Go Code Review Comments](https://github.com/golang/go/wiki/CodeReviewComments)
- [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md)

### Code Formatting

**All code must be formatted with `gofmt`:**

```bash
gofmt -w .
```

**Use `goimports` for import formatting:**

```bash
goimports -w .
```

### Naming Conventions

**Variables:**
```go
// Good
userID := "123"
httpClient := client.NewHTTPClient()

// Bad
userId := "123"        // Use camelCase, not snake_case
http_client := ...     // Use camelCase
```

**Functions:**
```go
// Good
func GetUser(id string) (*User, error)
func ValidateToken(token string) bool

// Bad
func get_user(id string) (*User, error)  // Use PascalCase for exported
func validatetoken(token string) bool     // Clear word boundaries
```

**Constants:**
```go
// Good
const MaxRetries = 3
const defaultTimeout = 10 * time.Second

// Bad
const MAX_RETRIES = 3    // Use PascalCase, not SCREAMING_SNAKE_CASE
```

### Error Handling

**Always check errors:**

```go
// Good
value, err := store.Get(ctx, key)
if err != nil {
    return fmt.Errorf("failed to get key %q: %w", key, err)
}

// Bad
value, _ := store.Get(ctx, key)  // Don't ignore errors
```

**Use error wrapping:**

```go
// Good
if err := doSomething(); err != nil {
    return fmt.Errorf("doSomething failed: %w", err)
}

// Bad
if err := doSomething(); err != nil {
    return err  // Lost context
}
```

### Comments

**Exported functions must have godoc comments:**

```go
// GetUser retrieves a user by ID from the database.
// Returns ErrNotFound if the user does not exist.
func GetUser(id string) (*User, error) {
    // ...
}
```

**Complex logic should have inline comments:**

```go
// Accumulate writes for 10ms to batch them together.
// This reduces fsync() calls from N to 1 per batch.
for {
    select {
    case entry := <-bw.entryCh:
        batch = append(batch, entry)
        // ...
    }
}
```

### Logging

**Use structured logging with Zap:**

```go
// Good
logger.Info("Applied Raft entry",
    zap.String("op", "put"),
    zap.String("key", key),
    zap.Uint64("index", index),
)

// Bad
logger.Info(fmt.Sprintf("Applied Raft entry: op=%s key=%s", op, key))
```

**Log levels:**

- `Debug`: Internal state changes, cache hits/misses
- `Info`: Operations, cluster events
- `Warn`: Recoverable errors
- `Error`: Serious errors that require attention

### Context Usage

**Always pass context:**

```go
// Good
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
    // ...
}

// Bad
func (s *Store) Get(key string) ([]byte, error) {
    // Missing context
}
```

**Check context cancellation:**

```go
select {
case <-ctx.Done():
    return ctx.Err()
case result := <-resultCh:
    return result, nil
}
```

---

## Testing Guidelines

### Test Organization

**File Naming:**
- Test files: `*_test.go`
- Benchmark files: `*_bench_test.go`
- Integration tests: `test/integration/*_test.go`

**Test Function Naming:**

```go
// Unit tests
func TestStorePut(t *testing.T) { ... }
func TestStoreGet_NotFound(t *testing.T) { ... }

// Benchmarks
func BenchmarkStorePut(b *testing.B) { ... }
```

### Unit Tests

**Example:**

```go
func TestMemoryStoreGet(t *testing.T) {
    // Arrange
    store := storage.NewMemoryStore()
    ctx := context.Background()
    key := "test-key"
    value := []byte("test-value")

    // Act
    err := store.Put(ctx, key, value)
    require.NoError(t, err)

    result, err := store.Get(ctx, key)

    // Assert
    require.NoError(t, err)
    assert.Equal(t, value, result)
}
```

**Use table-driven tests for multiple cases:**

```go
func TestValidateKey(t *testing.T) {
    tests := []struct {
        name    string
        key     string
        wantErr bool
    }{
        {"valid key", "user:123", false},
        {"empty key", "", true},
        {"too long", strings.Repeat("a", 1000), true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := ValidateKey(tt.key)
            if tt.wantErr {
                assert.Error(t, err)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Integration Tests

**Example:**

```go
func TestRaftClusterFormation(t *testing.T) {
    // Skip in short mode
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }

    // Setup cluster
    cluster := setupTestCluster(t, 3)
    defer cluster.Shutdown()

    // Wait for leader election
    leader := cluster.WaitForLeader(t, 10*time.Second)
    require.NotNil(t, leader)

    // Verify cluster
    nodes := cluster.GetNodes()
    assert.Len(t, nodes, 3)
}
```

**Run integration tests:**

```bash
# Run all tests
make test

# Run only integration tests
make test-integration

# Skip integration tests
go test -short ./...
```

### Benchmarks

**Example:**

```go
func BenchmarkStorePut(b *testing.B) {
    store := storage.NewMemoryStore()
    ctx := context.Background()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        key := fmt.Sprintf("key-%d", i)
        value := []byte("value")
        store.Put(ctx, key, value)
    }
}
```

**Run benchmarks:**

```bash
# Run all benchmarks
make bench

# Run specific benchmark
go test -bench=BenchmarkStorePut -benchmem ./internal/storage/

# Profile CPU
go test -bench=. -cpuprofile=cpu.out ./...
go tool pprof cpu.out
```

### Test Coverage

**Requirements:**
- Minimum **80% coverage** for new code
- **100% coverage** for critical paths (auth, storage)

**Check coverage:**

```bash
# Generate coverage report
make test-coverage

# View HTML report
go tool cover -html=coverage.out

# Check coverage percentage
go test -cover ./...
```

### Test Utilities

**Use testify for assertions:**

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// require: Test stops on failure
require.NoError(t, err)
require.NotNil(t, result)

// assert: Test continues on failure
assert.Equal(t, expected, actual)
assert.True(t, condition)
```

---

## Pull Request Process

### Before Submitting

**1. Create a feature branch:**

```bash
# Update main
git checkout main
git pull upstream main

# Create feature branch
git checkout -b feature/my-awesome-feature
```

**2. Make your changes:**

```bash
# Edit files
# ...

# Format code
make fmt

# Run linter
make lint

# Run tests
make test

# Run benchmarks (if performance-critical)
make bench
```

**3. Commit your changes:**

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "feat: add batched write support

- Implement BatchedWAL wrapper
- Add batch size and timeout configuration
- Update metrics to track batch operations
- Add benchmarks showing 10x improvement

Closes #123"
```

**Commit Message Format:**

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code formatting (no functional changes)
- `refactor`: Code restructuring
- `perf`: Performance improvements
- `test`: Adding/updating tests
- `chore`: Build process, dependencies

**Example:**
```
feat(storage): implement WAL compaction

Add automatic WAL compaction after snapshots to prevent
unbounded disk growth. Compaction deletes segments before
the snapshot index while keeping a safety margin.

- Add compaction_enabled config option
- Add compaction_margin config option
- Add metrics for compacted entries
- Add tests for compaction logic

Closes #45
```

### Submitting PR

**1. Push to your fork:**

```bash
git push origin feature/my-awesome-feature
```

**2. Create Pull Request on GitHub:**

- Navigate to https://github.com/RashikAnsar/raftkv
- Click "Compare & pull request"
- Fill in PR template:

```markdown
## Description

Brief description of changes.

## Motivation and Context

Why is this change needed? What problem does it solve?

Fixes #(issue number)

## Type of Change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to not work as expected)
- [ ] Documentation update

## How Has This Been Tested?

Describe the tests you ran to verify your changes.

- [ ] Unit tests pass (`make test`)
- [ ] Integration tests pass (`make test-integration`)
- [ ] Benchmarks run (`make bench`)
- [ ] Manual testing in local cluster

## Checklist

- [ ] My code follows the project's coding guidelines
- [ ] I have performed a self-review of my code
- [ ] I have commented my code, particularly in hard-to-understand areas
- [ ] I have made corresponding changes to the documentation
- [ ] My changes generate no new warnings
- [ ] I have added tests that prove my fix is effective or that my feature works
- [ ] New and existing unit tests pass locally with my changes
- [ ] Any dependent changes have been merged and published
```

### Code Review

**PR Requirements:**
- [x] All tests pass
- [x] No merge conflicts
- [x] Code coverage ≥80%
- [x] Linter passes
- [x] At least 1 approval from maintainer

**Review Process:**

1. **Automated checks** run (CI/CD)
2. **Code review** by maintainer(s)
3. **Address feedback** if needed
4. **Approval** from maintainer
5. **Merge** to main branch

**Addressing Feedback:**

```bash
# Make requested changes
# ...

# Amend commit (if minor)
git add .
git commit --amend

# Or add new commit
git commit -m "fix: address review feedback"

# Force push (if amended)
git push origin feature/my-awesome-feature --force-with-lease
```

### After Merge

**1. Delete branch:**

```bash
git checkout main
git pull upstream main
git branch -d feature/my-awesome-feature
git push origin --delete feature/my-awesome-feature
```

**2. Update your fork:**

```bash
git pull upstream main
git push origin main
```

---

## Release Process

### Versioning

RaftKV follows [Semantic Versioning 2.0.0](https://semver.org/):

- **MAJOR**: Incompatible API changes
- **MINOR**: Backwards-compatible new features
- **PATCH**: Backwards-compatible bug fixes

**Example:**
- `v1.0.0` → Initial release
- `v1.1.0` → Add new feature (backwards-compatible)
- `v1.1.1` → Fix bug (backwards-compatible)
- `v2.0.0` → Breaking change

### Release Checklist

**For Maintainers:**

1. **Update version:**
   ```bash
   # Update version in main.go
   VERSION="v1.1.0"
   ```

2. **Update CHANGELOG.md:**
   ```markdown
   ## [1.1.0] - 2025-01-10

   ### Added
   - WAL compaction feature
   - Batched write support

   ### Fixed
   - Memory leak in cache

   ### Changed
   - Improved snapshot performance
   ```

3. **Create release tag:**
   ```bash
   git tag -a v1.1.0 -m "Release v1.1.0"
   git push upstream v1.1.0
   ```

4. **Build release binaries:**
   ```bash
   make release
   ```

5. **Create GitHub release:**
   - Navigate to https://github.com/RashikAnsar/raftkv/releases
   - Click "Draft a new release"
   - Upload binaries
   - Publish release

---

## Getting Help

### Communication Channels

- **GitHub Issues**: https://github.com/RashikAnsar/raftkv/issues
- **GitHub Discussions**: https://github.com/RashikAnsar/raftkv/discussions

### Asking Questions

**Before asking:**
1. Search existing issues
2. Check documentation
3. Read FAQ

**When asking:**
1. Provide clear description
2. Include steps to reproduce
3. Share relevant logs
4. Mention Go version, OS, etc.

### Reporting Bugs

**Use this template:**

```markdown
**Describe the bug**
A clear description of what the bug is.

**To Reproduce**
Steps to reproduce the behavior:
1. Start cluster with '...'
2. Execute '...'
3. See error

**Expected behavior**
What you expected to happen.

**Actual behavior**
What actually happened.

**Logs**
```
Paste relevant logs here
```

**Environment**
- RaftKV version: v1.0.0
- Go version: 1.21.5
- OS: Ubuntu 22.04
- Deployment: Docker Compose

**Additional context**
Any other context about the problem.
```

### Suggesting Features

**Use this template:**

```markdown
**Is your feature request related to a problem?**
A clear description of what the problem is.

**Describe the solution you'd like**
A clear description of what you want to happen.

**Describe alternatives you've considered**
Any alternative solutions you've considered.

**Additional context**
Any other context about the feature request.
```

---

## License

By contributing to RaftKV, you agree that your contributions will be licensed under the MIT License.

---

## Acknowledgments

Thank you for contributing to RaftKV! Your contributions help make this project better for everyone.

**Special thanks to all contributors:**

- [List of contributors](https://github.com/RashikAnsar/raftkv/graphs/contributors)
