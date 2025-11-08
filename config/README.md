# RaftKV Configuration Files

This directory contains example configuration files for RaftKV.

## Available Configurations

### config.example.yaml
Comprehensive example showing **all available options** with detailed comments. Use this as a reference when creating your own configuration.

### config.dev.yaml
Development configuration with:
- Debug logging enabled
- Auth/TLS disabled for easier development
- Faster snapshots for testing
- Single-node mode
- Relaxed performance settings

**Usage:**
```bash
./kvstore --config config/config.dev.yaml
```

### config.prod.yaml
Production template with:
- Security enabled (TLS + Auth)
- Environment variable placeholders for secrets
- Clustering enabled
- Production-ready defaults

**IMPORTANT:** Copy this file and customize it for your environment. Never commit secrets!

```bash
# Copy and customize
cp config/config.prod.yaml /etc/raftkv/config.yaml

# Set secrets via environment variables
export RAFTKV_JWT_SECRET="your-secret-here"

# Run
./kvstore --config /etc/raftkv/config.yaml
```

## Creating Your Own Configuration

1. Copy an example file:
   ```bash
   cp config/config.example.yaml myconfig.yaml
   ```

2. Edit values as needed

3. Run with your config:
   ```bash
   ./kvstore --config myconfig.yaml
   ```

## Configuration Precedence

Configuration values are loaded in this order (highest priority first):

1. **Command-line flags** - `--server.http-addr :9000`
2. **Environment variables** - `RAFTKV_SERVER__HTTP_ADDR=:9000`
3. **Config file** - Values from `--config file.yaml`
4. **Defaults** - Built-in defaults

## Environment Variables

Use double underscore (`__`) for nested keys:

```bash
# server.http_addr
export RAFTKV_SERVER__HTTP_ADDR=":8080"

# auth.jwt_secret
export RAFTKV_AUTH__JWT_SECRET="my-secret"

# wal.batch_size
export RAFTKV_WAL__BATCH_SIZE=200
```

## Quick Examples

### Single-node with auth
```bash
./kvstore --config config/config.dev.yaml \
  --auth.enabled \
  --auth.jwt-secret "my-dev-secret"
```

### 3-node cluster (node 1 - bootstrap)
```bash
./kvstore --config config/config.prod.yaml \
  --raft.enabled \
  --raft.node-id node1 \
  --raft.raft-addr 192.168.1.10:7000 \
  --raft.bootstrap
```

### 3-node cluster (node 2 - join)
```bash
./kvstore --config config/config.prod.yaml \
  --raft.enabled \
  --raft.node-id node2 \
  --raft.raft-addr 192.168.1.11:7000 \
  --raft.join-addr 192.168.1.10:7000
```
