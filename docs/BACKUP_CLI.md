# kvbackup CLI Tool - Complete Guide

The `kvbackup` CLI tool provides command-line access to the RaftKV backup system. All core backup operations are now fully implemented and tested.

## Installation

```bash
# Build the CLI tool
go build -o kvbackup ./cmd/kvbackup/

# Optional: Install globally
sudo cp kvbackup /usr/local/bin/
```

## Quick Start

```bash
# 1. Create a backup
./kvbackup create --config backup.yaml --db ./raft-data/snapshot.gob

# 2. List all backups
./kvbackup list --config backup.yaml

# 3. Verify backup integrity
./kvbackup verify --config backup.yaml --backup <ID>

# 4. Restore from backup
./kvbackup restore --config backup.yaml --backup latest --db ./restored.gob

# 5. Delete old backup
./kvbackup delete --config backup.yaml --backup <ID> --force
```

## Commands Reference

### `create` - Create New Backup

Create a new backup from a database file.

**Syntax:**
```bash
kvbackup create --config <config-file> --db <database-file> [--output <path>]
```

**Flags:**
- `-c, --config` (required) - Path to backup configuration file
- `-d, --db` (required) - Path to database file to backup
- `-o, --output` (optional) - Custom output path for backup

**Example:**
```bash
$ ./kvbackup create --config backup.yaml --db ./raft-data/snapshot.gob

✓ Backup created successfully!

  Backup ID:       567b2ed9-d99c-47ef-a161-d37a70913fd2
  Timestamp:       2025-12-06T00:05:40+05:30
  Original Size:   129 bytes
  Compressed Size: 129 bytes
  Compression:     0.00%
  Encrypted:       false
  Status:          complete
  Checksum:        a4037ee72a4fafb4...
```

**What it does:**
1. Reads the database file
2. Compresses data (if enabled)
3. Encrypts data (if enabled)
4. Calculates SHA256 checksum
5. Uploads to configured storage
6. Saves metadata

---

### `list` - List All Backups

Display all available backups with metadata.

**Syntax:**
```bash
kvbackup list --config <config-file> [--format <format>]
```

**Flags:**
- `-c, --config` (required) - Path to backup configuration file
- `-f, --format` (optional) - Output format: table (default), json, yaml

**Example:**
```bash
$ ./kvbackup list --config backup.yaml

Found 2 backup(s):

ID                   TIMESTAMP            SIZE  COMPRESSED  ENCRYPTED  STATUS
--                   ---------            ----  ----------  ---------  ------
567b2ed9-d99c-47...  2025-12-06 00:05:40  129   true        false      complete
e984e6ef-825c-42...  2025-12-05 21:27:46  133   true        false      complete
```

**Table Columns:**
- **ID** - Unique backup identifier (truncated for display)
- **TIMESTAMP** - When backup was created
- **SIZE** - Compressed size in bytes
- **COMPRESSED** - Whether compression was applied
- **ENCRYPTED** - Whether encryption was applied
- **STATUS** - Backup status (complete, failed, in_progress)

---

### `verify` - Verify Backup Integrity

Verify backup integrity by checking checksums.

**Syntax:**
```bash
kvbackup verify --config <config-file> --backup <backup-id|all>
```

**Flags:**
- `-c, --config` (required) - Path to backup configuration file
- `-b, --backup` (required) - Backup ID to verify, or 'all' for all backups

**Example (Single Backup):**
```bash
$ ./kvbackup verify --config backup.yaml --backup 567b2ed9-d99c-47ef-a161-d37a70913fd2

✓ Backup 567b2ed9-d99c-47ef-a161-d37a70913fd2 verified successfully!
  Checksum: OK
```

**Example (All Backups):**
```bash
$ ./kvbackup verify --config backup.yaml --backup all

Verifying 2 backup(s)...

[1/2] Verifying 567b2ed9-d99c-47... OK
[2/2] Verifying e984e6ef-825c-42... OK

✓ Verification complete: 2 passed, 0 failed
```

**What it checks:**
- Downloads backup data
- Recalculates SHA256 checksum
- Compares with stored checksum
- Reports any corruption

---

### `restore` - Restore from Backup

Restore data from a backup to a file.

**Syntax:**
```bash
kvbackup restore --config <config-file> --backup <backup-id|latest> --db <output-path> [--force]
```

**Flags:**
- `-c, --config` (required) - Path to backup configuration file
- `-b, --backup` (required) - Backup ID to restore, or 'latest' for most recent
- `-d, --db` (required) - Path to restore database to
- `-f, --force` (optional) - Overwrite existing file without confirmation

**Example:**
```bash
$ ./kvbackup restore --config backup.yaml --backup latest --db ./restored.gob

✓ Backup restored successfully!

  Backup ID:    567b2ed9-d99c-47ef-a161-d37a70913fd2
  Output File:  ./restored.gob
  Size:         138 bytes
```

**Using 'latest' keyword:**
The `--backup latest` flag automatically selects the most recent backup:
```bash
# These are equivalent if 567b2ed9... is the latest backup:
./kvbackup restore --backup latest --db ./restored.gob --config backup.yaml
./kvbackup restore --backup 567b2ed9-d99c-47ef-a161-d37a70913fd2 --db ./restored.gob --config backup.yaml
```

**Restore Process:**
1. Downloads encrypted data
2. Decrypts data (if encrypted)
3. Decompresses data (if compressed)
4. Writes to output file
5. Verifies file size

**Safety:**
- Won't overwrite existing files unless `--force` is used
- Creates parent directories automatically
- Cleans up on failure

---

### `delete` - Delete Backup

Delete a specific backup from storage.

**Syntax:**
```bash
kvbackup delete --config <config-file> --backup <backup-id> [--force]
```

**Flags:**
- `-c, --config` (required) - Path to backup configuration file
- `-b, --backup` (required) - Backup ID to delete
- `-f, --force` (optional) - Skip confirmation prompt

**Example (with confirmation):**
```bash
$ ./kvbackup delete --config backup.yaml --backup e984e6ef-825c-42d4-96e6-e1f7ecb8a6a8

Are you sure you want to delete backup e984e6ef-825c-42d4-96e6-e1f7ecb8a6a8? (yes/no): yes

✓ Backup e984e6ef-825c-42d4-96e6-e1f7ecb8a6a8 deleted successfully!
```

**Example (without confirmation):**
```bash
$ ./kvbackup delete --config backup.yaml --backup e984e6ef-825c-42d4-96e6-e1f7ecb8a6a8 --force

✓ Backup e984e6ef-825c-42d4-96e6-e1f7ecb8a6a8 deleted successfully!
```

**What it deletes:**
- Backup data file
- Backup metadata
- All associated files

**Warning:** Deletion is permanent and cannot be undone!

---

### `schedule` - Manage Automated Backups

Manage automated backup scheduling (requires integration with cron/systemd).

**Subcommands:**
- `schedule start` - Start backup scheduler
- `schedule stop` - Stop backup scheduler
- `schedule status` - Show scheduler status

**Note:** Full scheduler implementation requires integration with system services. For now, use cron or systemd timers to run `kvbackup create` periodically.

**Example with cron:**
```bash
# Edit crontab
crontab -e

# Add daily backup at 2 AM
0 2 * * * /usr/local/bin/kvbackup create --config /etc/raftkv/backup.yaml --db /var/lib/raftkv/snapshot.gob
```

**Example with systemd timer:**
```ini
# /etc/systemd/system/raftkv-backup.timer
[Unit]
Description=RaftKV Daily Backup

[Timer]
OnCalendar=daily
OnCalendar=02:00
Persistent=true

[Install]
WantedBy=timers.target
```

---

## Configuration File

The CLI tool uses YAML configuration files:

```yaml
provider:
  type: local
  local_path: /var/lib/raftkv/backups

backup:
  compression:
    enabled: true
    type: gzip
    level: 6
  encryption:
    enabled: false
  node_id: node-1
  cluster_id: production
  version: 1.0.0
  prefix: backups/

retention:
  retention_days: 7
  max_backups: 5
  min_backups: 2
  prune_on_backup: true
```

See [BACKUP_QUICKSTART.md](BACKUP_QUICKSTART.md) for detailed configuration options.

---

## Common Workflows

### Daily Backup Routine

```bash
#!/bin/bash
# Daily backup script

CONFIG="/etc/raftkv/backup.yaml"
DB_PATH="/var/lib/raftkv/data/snapshot.gob"

# Create backup
BACKUP_ID=$(./kvbackup create --config "$CONFIG" --db "$DB_PATH" | grep "Backup ID" | awk '{print $3}')

# Verify it
./kvbackup verify --config "$CONFIG" --backup "$BACKUP_ID"

# List all backups
./kvbackup list --config "$CONFIG"
```

### Disaster Recovery

```bash
#!/bin/bash
# Restore from latest backup

CONFIG="/etc/raftkv/backup.yaml"
RESTORE_PATH="/var/lib/raftkv/data/restored.gob"

# List available backups
./kvbackup list --config "$CONFIG"

# Restore latest
./kvbackup restore --config "$CONFIG" --backup latest --db "$RESTORE_PATH" --force

# Verify restored data
echo "Restored $(stat -f%z "$RESTORE_PATH") bytes"
```

### Backup Maintenance

```bash
#!/bin/bash
# Weekly backup maintenance

CONFIG="/etc/raftkv/backup.yaml"

# Verify all backups
./kvbackup verify --config "$CONFIG" --backup all

# List backups
./kvbackup list --config "$CONFIG"

# Delete specific old backup if needed
# ./kvbackup delete --config "$CONFIG" --backup <OLD_ID> --force
```

---

## Exit Codes

- `0` - Success
- `1` - Error (check error message)

---

## Troubleshooting

### "failed to load config: invalid configuration"

**Problem:** Configuration file is missing required fields.

**Solution:** Ensure all required fields are present:
```yaml
provider:
  type: local
  local_path: /path/to/backups  # Required for local provider
```

### "database file already exists (use --force to overwrite)"

**Problem:** Trying to restore to an existing file.

**Solution:** Use `--force` flag or choose a different output path:
```bash
./kvbackup restore --backup latest --db ./restored.gob --force --config backup.yaml
```

### "failed to create backup: permission denied"

**Problem:** No write permission to backup directory.

**Solution:** Check directory permissions:
```bash
chmod 755 /path/to/backups
# Or run with appropriate permissions
sudo ./kvbackup create --config backup.yaml --db snapshot.gob
```

### "verification failed: checksum mismatch"

**Problem:** Backup data is corrupted.

**Solution:** Delete corrupted backup and create a new one:
```bash
./kvbackup delete --backup <CORRUPTED_ID> --force --config backup.yaml
./kvbackup create --db snapshot.gob --config backup.yaml
```

---

## Advanced Usage

### Using with Different Storage Providers

**S3:**
```yaml
provider:
  type: s3
  s3_bucket: my-raftkv-backups
  s3_region: us-east-1
  s3_access_key: ${AWS_ACCESS_KEY_ID}
  s3_secret_key: ${AWS_SECRET_ACCESS_KEY}
```

**MinIO:**
```yaml
provider:
  type: minio
  s3_bucket: raftkv-backups
  s3_endpoint: http://minio.example.com:9000
  s3_access_key: minioadmin
  s3_secret_key: minioadmin
```

### Enabling Encryption

```yaml
backup:
  encryption:
    enabled: true
    type: aes-256-gcm
    key_file: /etc/raftkv/encryption.key
```

Generate encryption key:
```bash
openssl rand -hex 32 > /etc/raftkv/encryption.key
chmod 600 /etc/raftkv/encryption.key
```

---

## Integration with RaftKV Server

To integrate with a running RaftKV server:

```bash
# Backup the latest snapshot
LATEST_SNAPSHOT=$(ls -t /var/lib/raftkv/snapshots/snapshot-*.gob | head -1)
./kvbackup create --config backup.yaml --db "$LATEST_SNAPSHOT"
```

---

## Performance Tips

1. **Use compression for large datasets** - Reduces storage costs and transfer time
2. **Enable encryption for sensitive data** - Adds ~10% overhead
3. **Use local provider for fast backups** - Then sync to S3/MinIO async
4. **Verify backups periodically** - Not every time (expensive for large backups)
5. **Use retention policies** - Automatically prune old backups

---

## Security Best Practices

1. **Protect config files** - `chmod 600 backup.yaml`
2. **Use encryption for production** - Especially for remote storage
3. **Rotate encryption keys** - Periodically update encryption keys
4. **Limit backup access** - Use IAM roles/policies for S3
5. **Monitor backup operations** - Log all backup activities

---

## See Also

- [DEEP_DIVE_SNAPSHOTS_BACKUPS.md](DEEP_DIVE_SNAPSHOTS_BACKUPS.md) - Technical deep dive
<!-- - [BACKUP_QUICKSTART.md](BACKUP_QUICKSTART.md) - Quick start guide -->
<!-- - [BACKUP_DEMO_SUMMARY.md](BACKUP_DEMO_SUMMARY.md) - Demo results and examples
- [examples/backup-example.go](../examples/backup-example.go) - Go API examples -->
