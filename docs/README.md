# RaftKV Documentation

> Complete documentation for RaftKV - A production-grade distributed key-value store

## Welcome

Welcome to the RaftKV documentation! This collection provides everything you need to understand, deploy, operate, and contribute to RaftKV.

## Quick Links

| Document                                  | Description                       | Audience               |
| ----------------------------------------- | --------------------------------- | ---------------------- |
| [**README.md**](../README.md)             | Project overview and quick start  | Everyone               |
| [**ARCHITECTURE.md**](ARCHITECTURE.md)    | System architecture with diagrams | Developers, Architects |
| [**API_REFERENCE.md**](API_REFERENCE.md)  | Complete API documentation        | Developers, API Users  |
| [**DEPLOYMENT.md**](DEPLOYMENT.md)        | Deployment instructions           | DevOps, SRE            |
| [**OPERATIONS.md**](OPERATIONS.md)        | Operations runbook                | Operations, SRE        |
| [**CONTRIBUTING.md**](../CONTRIBUTING.md) | Contribution guidelines           | Contributors           |

---

## Documentation by Role

### 🚀 For Users & Developers

**Getting Started:**
1. Read the [README](../README.md) for project overview
2. Check out [Quick Start](../README.md#quick-start) to run your first node
3. Explore the [API Reference](API_REFERENCE.md) for API usage

**Development:**
- [Architecture Overview](ARCHITECTURE.md) - Understand system design
- [API Reference](API_REFERENCE.md) - HTTP REST and gRPC APIs
- [Configuration Guide](../config/README.md) - All configuration options
- [Client Libraries](../pkg/client/) - Go client SDK

### 🔧 For DevOps & SRE

**Deployment:**
- [Deployment Guide](DEPLOYMENT.md) - Single-node, cluster, Docker, K8s
- [TLS/mTLS Setup](DEPLOYMENT.md#tlsmtls-setup) - Security configuration
- [Authentication Setup](DEPLOYMENT.md#authentication-setup) - User and API key management

**Operations:**
- [Operations Runbook](OPERATIONS.md) - Day-to-day operations
- [Monitoring Guide](OPERATIONS.md#monitoring) - Prometheus + Grafana setup
- [Backup & Restore](OPERATIONS.md#backup--restore) - Data protection procedures
- [Troubleshooting](OPERATIONS.md#troubleshooting) - Common issues and solutions

### 👨‍💻 For Contributors

**Contributing:**
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute
- [Development Setup](../CONTRIBUTING.md#development-setup) - Local development
- [Coding Guidelines](../CONTRIBUTING.md#coding-guidelines) - Code standards
- [Testing Guidelines](../CONTRIBUTING.md#testing-guidelines) - Writing tests

---

## Table of Contents

### Core Documentation

#### 1. [README.md](../README.md)
Project overview, features, quick start, and roadmap.

**Contents:**
- Features and status
- Quick start (single-node and cluster)
- Configuration overview
- Monitoring setup
- Performance benchmarks
- Development instructions

#### 2. [ARCHITECTURE.md](ARCHITECTURE.md)
Complete system architecture with detailed diagrams.

**Contents:**
- Executive summary
- System architecture (high-level and component hierarchy)
- Core components (storage engine, Raft consensus, API servers)
- Data flow (write operations, read operations, leader failover)
- Deployment architectures
- Performance characteristics
- Security architecture
- Monitoring and observability
- Technology stack
- Architectural decisions

#### 3. [API_REFERENCE.md](API_REFERENCE.md)
Complete HTTP REST and gRPC API documentation.

**Contents:**
- **HTTP REST API:**
  - Key-value operations (GET, PUT, DELETE, List)
  - Cluster management (join, remove, nodes, leader)
  - Admin endpoints (snapshot, stats, health)
  - Authentication endpoints (login, refresh, logout)
  - User management (create, list, update, delete)
  - API key management (create, list, revoke)
- **gRPC API:**
  - Service definitions
  - Method documentation
  - Example code (Go, grpcurl)
- **Error Handling:**
  - HTTP status codes
  - gRPC error codes
  - Leader redirection

#### 4. [DEPLOYMENT.md](DEPLOYMENT.md)
Complete deployment guide for all environments.

**Contents:**
- Prerequisites (system and software requirements)
- **Single-Node Deployment:**
  - Binary installation
  - Configuration
  - systemd service setup
  - Verification
- **3-Node Cluster Deployment:**
  - Architecture overview
  - Node configuration (bootstrap and join)
  - Load balancer setup (HAProxy)
- **Docker Deployment:**
  - Single-node container
  - 3-node Docker Compose
  - Monitoring stack
- **TLS/mTLS Setup:**
  - Certificate generation
  - Configuration
  - Testing
- **Authentication Setup:**
  - Enabling authentication
  - User management
  - API key creation
- **Production Checklist**
- **Troubleshooting**

#### 5. [OPERATIONS.md](OPERATIONS.md)
Operations runbook for production environments.

**Contents:**
- **Monitoring:**
  - Prometheus setup
  - Alert rules (critical and warning)
  - Grafana dashboards
  - Key queries
- **Backup & Restore:**
  - Manual snapshots
  - Automatic snapshots
  - Backup procedures (single-node and cluster)
  - Restore procedures
  - Disaster recovery
- **Performance Tuning:**
  - Identifying bottlenecks
  - Tuning parameters (WAL batching, cache, snapshots)
  - Benchmarking
- **Cluster Operations:**
  - Add/remove nodes
  - Leader election
  - Cluster health checks
- **Troubleshooting:**
  - High write latency
  - Low cache hit rate
  - WAL growing unbounded
  - No Raft leader elected
  - Service won't start
- **Incident Response:**
  - Severity levels
  - Incident playbooks
  - On-call runbook
- **Maintenance Procedures:**
  - Rolling upgrades
  - Log rotation
  - Regular tasks

#### 6. [CONTRIBUTING.md](../CONTRIBUTING.md)
Contribution guidelines for developers.

**Contents:**
- Code of conduct
- Getting started (prerequisites, fork & clone)
- **Development Setup:**
  - Build from source
  - Running tests
  - Local development
  - Development tools
- **Project Structure:**
  - Directory layout
  - Key packages
- **Coding Guidelines:**
  - Go style guide
  - Code formatting
  - Naming conventions
  - Error handling
  - Comments and logging
- **Testing Guidelines:**
  - Unit tests
  - Integration tests
  - Benchmarks
  - Test coverage
- **Pull Request Process:**
  - Before submitting
  - Commit message format
  - Submitting PR
  - Code review
- **Release Process:**
  - Versioning (Semantic Versioning)
  - Release checklist

---

## Additional Resources

### Configuration

- [Configuration Guide](../config/README.md) - Complete configuration reference
- [Example Configs](../config/) - Development and production examples
  - [config.example.yaml](../config/config.example.yaml) - Annotated example
  - [config.dev.yaml](../config/config.dev.yaml) - Development preset
  - [config.prod.yaml](../config/config.prod.yaml) - Production preset

### Deployment

- [Docker Compose](../deployments/docker/) - Containerized deployment
  - [docker-compose.simple.yml](../deployments/docker/docker-compose.simple.yml) - 3-node cluster
  - [docker-compose.yml](../deployments/docker/docker-compose.yml) - With HAProxy
  - [docker-compose.monitoring.yml](../deployments/docker/docker-compose.monitoring.yml) - With monitoring
- [Kubernetes](../deployments/kubernetes/) - K8s manifests (future)
- [Monitoring](../deployments/monitoring/) - Prometheus and Grafana configs

### Scripts

- [benchmark.sh](../scripts/benchmark.sh) - Performance benchmarking
- [generate-certs.sh](../scripts/generate-certs.sh) - TLS certificate generation
- [start-cluster.sh](../scripts/start-cluster.sh) - Start 3-node cluster
- [stop-cluster.sh](../scripts/stop-cluster.sh) - Stop cluster


## Contributing to Documentation

Found an issue or want to improve the documentation? Contributions are welcome!

1. **Typos & fixes**: Open a PR with corrections
2. **Missing content**: Open an issue describing what's missing
3. **New guides**: Discuss in an issue before writing

**Documentation Standards:**
- Use clear, concise language
- Include code examples
- Add diagrams where helpful (mermaid preferred)
- Keep consistent formatting
- Test all commands and examples


---

## License

RaftKV is released under the **MIT License**. See [LICENSE](../LICENSE) for details.

By contributing to RaftKV, you agree that your contributions will be licensed under the MIT License.

## Acknowledgments

RaftKV is built on the shoulders of giants:

- **HashiCorp Raft** - Consensus implementation
- **Gorilla Mux** - HTTP routing
- **Uber Zap** - Structured logging
- **Prometheus** - Metrics collection
- **gRPC** - Binary RPC protocol


## Quick Navigation

### By Topic

**Getting Started:**
- [Quick Start](../README.md#quick-start)
- [Installation](DEPLOYMENT.md#prerequisites)
- [Configuration](../config/README.md)

**Development:**
- [Architecture](ARCHITECTURE.md)
- [API Reference](API_REFERENCE.md)
- [Contributing](../CONTRIBUTING.md)

**Operations:**
- [Deployment](DEPLOYMENT.md)
- [Monitoring](OPERATIONS.md#monitoring)
- [Troubleshooting](OPERATIONS.md#troubleshooting)

**Security:**
- [TLS Setup](DEPLOYMENT.md#tlsmtls-setup)
- [Authentication](DEPLOYMENT.md#authentication-setup)
- [RBAC](API_REFERENCE.md#authorization-roles-rbac)
