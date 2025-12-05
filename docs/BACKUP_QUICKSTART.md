# Backup Quickstart Guide

A practical guide to creating and managing local backups in RaftKV.

---

## Quick Start (5 Minutes)

### Step 1: Create Configuration File

Create a backup configuration file:

```bash
cat > backup-local.yaml << 'EOF'
# Local backup configuration for development/testing
provider:
  type: local
  local_path: /tmp/raftkv-backups

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: false  # Disabled for simplicity (enable in production!)
  node_id: node-1
  cluster_id: raftkv-local
  version: 1.0.0
  prefix: backups/

schedule:
  enabled: false  # Manual backups for now

retention:
  retention_days: 7
  max_backups: 5
  min_backups: 2
  prune_on_backup: true
EOF
```

### Step 2: Build the CLI Tool

```bash
# Build the kvbackup CLI tool
cd raftkv/
go build -o kvbackup ./cmd/kvbackup/
```

### Step 3: Create Your First Backup

```bash
# Create a manual backup
./kvbackup create \
  --config backup-local.yaml \
  --db ./raft-data
```

**Expected Output**:
```
Creating backup from ./raft-data with config backup-local.yaml
✓ Backup created successfully
  ID:         d90ad4b2-5a96-475e-85f1-329ec2662b35
  Size:       102.5 MB → 315 KB (0.3% compression ratio)
  Location:   /tmp/raftkv-backups/backups/d90ad4b2.../data.db
  Checksum:   a3f2...9bc1
  Duration:   2.3s
```

### Step 4: List Your Backups

```bash
./kvbackup list --config backup-local.yaml
```

**Expected Output**:
```
BACKUP ID                              TIMESTAMP           SIZE      COMPRESSED  ENCRYPTED  STATUS
d90ad4b2-5a96-475e-85f1-329ec2662b35  2025-12-05 10:30   315 KB    Yes         No         complete
7f3e1a9c-2d4b-4f8e-9a7c-5e6d8f9a0b1c  2025-12-04 10:30   310 KB    Yes         No         complete
```

### Step 5: Verify Backup Integrity

```bash
./kvbackup verify \
  --config backup-local.yaml \
  --backup d90ad4b2-5a96-475e-85f1-329ec2662b35
```

### Step 6: Restore from Backup

```bash
./kvbackup restore \
  --config backup-local.yaml \
  --backup latest \
  --db ./raft-data-restored
```

---

## Method 1: Using the CLI Tool (Recommended)

### Prerequisites

1. **RaftKV is running** (or you have existing raft-data)
2. **Go 1.21+** installed
3. **Write access** to backup destination

### Installation

```bash
# Clone repository (if not already)
git clone https://github.com/RashikAnsar/raftkv.git
cd raftkv

# Build the backup CLI
go build -o kvbackup ./cmd/kvbackup/

# Make it executable
chmod +x kvbackup

# Optional: Install globally
sudo mv kvbackup /usr/local/bin/
```

### Configuration Options

#### Option A: Minimal Configuration (No Encryption)

```yaml
# backup-simple.yaml
provider:
  type: local
  local_path: ./backups

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: false
  node_id: node-1
  cluster_id: my-cluster
  version: 1.0.0
  prefix: backups/

retention:
  max_backups: 5
```

#### Option B: Secure Configuration (With Encryption)

```yaml
# backup-secure.yaml
provider:
  type: local
  local_path: ./backups

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: true
    type: aes-256-gcm
    key_file: ./backup-encryption.key  # 32-byte key file
  node_id: node-1
  cluster_id: my-cluster
  version: 1.0.0
  prefix: backups/

retention:
  retention_days: 30
  max_backups: 10
  min_backups: 3
```

**Generate encryption key**:
```bash
# Generate a 32-byte random key
openssl rand -base64 32 > backup-encryption.key

# Secure the key file
chmod 600 backup-encryption.key
```

### Common Operations

#### Create Backup

```bash
# Basic backup
kvbackup create --config backup.yaml --db ./raft-data

# With custom output location
kvbackup create \
  --config backup.yaml \
  --db ./raft-data \
  --output /mnt/external/backup-2025-12-05.db

# From running RaftKV instance
kvbackup create \
  --config backup.yaml \
  --db /var/lib/raftkv/raft-data
```

#### List Backups

```bash
# Table format (default)
kvbackup list --config backup.yaml

# JSON format
kvbackup list --config backup.yaml --format json

# YAML format
kvbackup list --config backup.yaml --format yaml
```

**JSON Output**:
```json
[
  {
    "id": "d90ad4b2-5a96-475e-85f1-329ec2662b35",
    "timestamp": "2025-12-05T10:30:45Z",
    "raft_index": 1000,
    "raft_term": 5,
    "size": 104857600,
    "compressed_size": 322560,
    "compressed": true,
    "encrypted": false,
    "node_id": "node-1",
    "cluster_id": "my-cluster",
    "checksum": "a3f2...9bc1",
    "status": "complete"
  }
]
```

#### Restore Backup

```bash
# Restore latest backup
kvbackup restore \
  --config backup.yaml \
  --backup latest \
  --db ./raft-data-restored

# Restore specific backup
kvbackup restore \
  --config backup.yaml \
  --backup d90ad4b2-5a96-475e-85f1-329ec2662b35 \
  --db ./raft-data-restored

# Force overwrite existing data
kvbackup restore \
  --config backup.yaml \
  --backup latest \
  --db ./raft-data \
  --force
```

#### Verify Backup

```bash
# Verify specific backup
kvbackup verify \
  --config backup.yaml \
  --backup d90ad4b2-5a96-475e-85f1-329ec2662b35

# Verify all backups
kvbackup verify \
  --config backup.yaml \
  --backup all
```

#### Delete Backup

```bash
# Delete with confirmation prompt
kvbackup delete \
  --config backup.yaml \
  --backup d90ad4b2-5a96-475e-85f1-329ec2662b35

# Force delete (skip confirmation)
kvbackup delete \
  --config backup.yaml \
  --backup d90ad4b2-5a96-475e-85f1-329ec2662b35 \
  --force
```

---

## Method 2: Using Go API Directly

If you want to integrate backup into your own Go code:

### Example: Create Backup Programmatically

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/RashikAnsar/raftkv/internal/backup"
    "github.com/RashikAnsar/raftkv/internal/backup/providers"
)

func main() {
    ctx := context.Background()

    // Step 1: Create local storage provider
    provider, err := providers.NewLocalProvider("./backups")
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Step 2: Configure backup settings
    config := backup.BackupConfig{
        CompressionType:  backup.CompressionTypeGzip,
        CompressionLevel: 6,
        EncryptionType:   backup.EncryptionTypeNone,
        NodeID:           "node-1",
        ClusterID:        "my-cluster",
        Version:          "1.0.0",
        RetentionDays:    30,
        MaxBackups:       10,
        BackupPrefix:     "backups/",
    }

    // Step 3: Create backup manager
    manager, err := backup.NewBackupManager(provider, config)
    if err != nil {
        log.Fatal(err)
    }
    defer manager.Close()

    // Step 4: Read snapshot data
    snapshotFile, err := os.Open("./raft-data/snapshots/snapshot-000001000.gob")
    if err != nil {
        log.Fatal(err)
    }
    defer snapshotFile.Close()

    // Step 5: Create backup
    metadata, err := manager.CreateBackup(ctx, snapshotFile, 1000, 5)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Backup created successfully!\n")
    fmt.Printf("  ID:         %s\n", metadata.ID)
    fmt.Printf("  Size:       %d bytes\n", metadata.Size)
    fmt.Printf("  Compressed: %d bytes\n", metadata.CompressedSize)
    fmt.Printf("  Checksum:   %s\n", metadata.Checksum)
}
```

### Example: List Backups

```go
func listBackups(manager *backup.BackupManager) {
    ctx := context.Background()

    backups, err := manager.ListBackups(ctx)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Found %d backups:\n\n", len(backups))
    for _, b := range backups {
        fmt.Printf("ID:        %s\n", b.ID)
        fmt.Printf("Timestamp: %s\n", b.Timestamp)
        fmt.Printf("Size:      %d KB\n", b.CompressedSize/1024)
        fmt.Printf("Status:    %s\n\n", b.Status)
    }
}
```

### Example: Restore Backup

```go
func restoreBackup(manager *backup.BackupManager, backupID string) {
    ctx := context.Background()

    // Create restore manager
    compressor, _ := backup.NewCompressor(backup.CompressionTypeGzip, 6)
    encryptor, _ := backup.NewEncryptor(backup.EncryptionTypeNone, nil)
    restorer := backup.NewRestoreManager(provider, compressor, encryptor)

    // Create output file
    outputFile, err := os.Create("./restored-snapshot.gob")
    if err != nil {
        log.Fatal(err)
    }
    defer outputFile.Close()

    // Restore
    err = restorer.RestoreBackup(ctx, backupID, outputFile)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Restore completed successfully!")
}
```

---

## Method 3: Automated Scheduled Backups

### Setup Cron-based Scheduling

```yaml
# backup-scheduled.yaml
provider:
  type: local
  local_path: /var/lib/raftkv/backups

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: true
    type: aes-256-gcm
    key_file: /etc/raftkv/backup.key
  node_id: node-1
  cluster_id: raftkv-prod
  version: 1.0.0
  prefix: backups/

schedule:
  enabled: true
  cron_expression: "0 0 * * *"  # Daily at midnight
  timezone: UTC
  enable_pruning: true

retention:
  retention_days: 30
  max_backups: 10
  min_backups: 3
  prune_on_backup: true
```

### Start Scheduler

```bash
# Start in foreground (testing)
kvbackup schedule start --config backup-scheduled.yaml

# Start as daemon (production)
kvbackup schedule start --config backup-scheduled.yaml --daemon

# Check status
kvbackup schedule status

# Stop scheduler
kvbackup schedule stop
```

### Using System Services

#### Systemd Service (Linux)

```ini
# /etc/systemd/system/raftkv-backup.service
[Unit]
Description=RaftKV Backup Scheduler
After=network.target raftkv.service
Requires=raftkv.service

[Service]
Type=simple
User=raftkv
Group=raftkv
WorkingDirectory=/var/lib/raftkv
ExecStart=/usr/local/bin/kvbackup schedule start \
  --config /etc/raftkv/backup.yaml \
  --daemon
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

**Enable and start**:
```bash
sudo systemctl enable raftkv-backup
sudo systemctl start raftkv-backup
sudo systemctl status raftkv-backup
```

#### Cron Job (Alternative)

```bash
# Edit crontab
crontab -e

# Add daily backup at midnight
0 0 * * * /usr/local/bin/kvbackup create \
  --config /etc/raftkv/backup.yaml \
  --db /var/lib/raftkv/raft-data \
  >> /var/log/raftkv-backup.log 2>&1
```

---

## Directory Structure After Backups

```
./backups/
├── backups/
│   ├── d90ad4b2-5a96-475e-85f1-329ec2662b35/
│   │   ├── data.db           # Compressed + encrypted backup
│   │   └── metadata.json     # Backup metadata
│   ├── 7f3e1a9c-2d4b-4f8e-9a7c-5e6d8f9a0b1c/
│   │   ├── data.db
│   │   └── metadata.json
│   └── a1b2c3d4-e5f6-4789-a0b1-c2d3e4f5g6h7/
│       ├── data.db
│       └── metadata.json
```

**Metadata Example** (`metadata.json`):
```json
{
  "id": "d90ad4b2-5a96-475e-85f1-329ec2662b35",
  "timestamp": "2025-12-05T10:30:45Z",
  "raft_index": 1000,
  "raft_term": 5,
  "size": 104857600,
  "compressed_size": 322560,
  "compressed": true,
  "encryption": false,
  "compression_type": "gzip",
  "encryption_type": "none",
  "node_id": "node-1",
  "cluster_id": "my-cluster",
  "version": "1.0.0",
  "checksum": "a3f2d8c4b7e9f1a5c8d2e6b3f9a7c5e1d4b8f2a6c9e3d7b1f5a8c2e6d3f9a7c5",
  "status": "complete"
}
```

---

## Troubleshooting

### Error: "Config file not found"

```bash
# Check file exists
ls -l backup.yaml

# Use absolute path
kvbackup create --config /full/path/to/backup.yaml --db ./raft-data
```

### Error: "Failed to create provider: base path cannot be empty"

```yaml
# Fix: Specify local_path in config
provider:
  type: local
  local_path: ./backups  # Add this line
```

### Error: "Encryption key must be 32 bytes"

```bash
# Generate correct key size
openssl rand -out backup.key 32  # Exactly 32 bytes

# Verify size
wc -c backup.key  # Should output: 32
```

### Error: "Database path does not exist"

```bash
# Check raft-data directory exists
ls -la ./raft-data

# Create if needed
mkdir -p ./raft-data/snapshots

# Use correct path
kvbackup create --config backup.yaml --db /var/lib/raftkv/raft-data
```

### Large Backup Takes Too Long

```yaml
# Reduce compression level for speed
backup:
  compression:
    enabled: true
    level: 1  # Fastest (level 9 = slowest/best compression)
```

### Restore Fails with Checksum Mismatch

```bash
# Verify backup integrity first
kvbackup verify --config backup.yaml --backup <ID>

# If corrupted, use earlier backup
kvbackup list --config backup.yaml
kvbackup restore --config backup.yaml --backup <earlier-ID> --db ./restored
```

---

## Best Practices

### Development

```yaml
provider:
  type: local
  local_path: ./backups
backup:
  compression:
    enabled: true
    level: 1  # Fast compression
  encryption:
    enabled: false  # Disable for speed
retention:
  max_backups: 3  # Keep only recent backups
```

### Testing/Staging

```yaml
provider:
  type: local
  local_path: /mnt/backups
backup:
  compression:
    enabled: true
    level: 6  # Balanced
  encryption:
    enabled: true  # Test encryption
schedule:
  enabled: true
  cron_expression: "0 */6 * * *"  # Every 6 hours
retention:
  max_backups: 10
```

### Production

```yaml
provider:
  type: s3  # Use cloud storage
  s3_bucket: prod-backups
  s3_region: us-east-1
backup:
  compression:
    enabled: true
    level: 6
  encryption:
    enabled: true  # Always encrypt in production!
schedule:
  enabled: true
  cron_expression: "0 0 * * *"  # Daily
retention:
  retention_days: 30
  max_backups: 10
  min_backups: 3
```

---

## Complete Example Script

Save this as `backup.sh`:

```bash
#!/bin/bash
set -e

# Configuration
CONFIG_FILE="backup.yaml"
RAFT_DATA="./raft-data"
BACKUP_DIR="./backups"

# Create backup directory
mkdir -p "$BACKUP_DIR"

# Create configuration if not exists
if [ ! -f "$CONFIG_FILE" ]; then
    cat > "$CONFIG_FILE" << EOF
provider:
  type: local
  local_path: $BACKUP_DIR

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: false
  node_id: node-1
  cluster_id: raftkv-local
  version: 1.0.0
  prefix: backups/

retention:
  max_backups: 5
EOF
    echo "✓ Created configuration: $CONFIG_FILE"
fi

# Build CLI if not exists
if [ ! -f "./kvbackup" ]; then
    echo "Building kvbackup CLI..."
    go build -o kvbackup ./cmd/kvbackup/
    echo "✓ Built kvbackup"
fi

# Create backup
echo "Creating backup..."
./kvbackup create --config "$CONFIG_FILE" --db "$RAFT_DATA"

# List backups
echo ""
echo "Available backups:"
./kvbackup list --config "$CONFIG_FILE"

# Verify latest backup
echo ""
echo "Verifying latest backup..."
LATEST_ID=$(./kvbackup list --config "$CONFIG_FILE" --format json | jq -r '.[0].id')
./kvbackup verify --config "$CONFIG_FILE" --backup "$LATEST_ID"

echo ""
echo "✓ Backup completed successfully!"
```

**Usage**:
```bash
chmod +x backup.sh
./backup.sh
```

---

## Next Steps

1. **Test locally**: Follow the Quick Start guide
2. **Set up encryption**: Generate key and enable encryption
3. **Schedule backups**: Use cron or systemd
4. **Test restore**: Verify you can restore successfully
5. **Monitor**: Add alerting for backup failures
6. **Move to cloud**: Configure S3/GCS for production

---

**Quick Reference Commands**:
```bash
# Create backup
kvbackup create --config backup.yaml --db ./raft-data

# List backups
kvbackup list --config backup.yaml

# Restore latest
kvbackup restore --config backup.yaml --backup latest --db ./restored

# Verify backup
kvbackup verify --config backup.yaml --backup <ID>

# Delete backup
kvbackup delete --config backup.yaml --backup <ID>

# Start scheduler
kvbackup schedule start --config backup.yaml --daemon
```

For more details, see:
- [Deep Dive Documentation](DEEP_DIVE_SNAPSHOTS_BACKUPS.md)
- [Example Configuration](../config/backup.example.yaml)
