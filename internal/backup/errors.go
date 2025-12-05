package backup

import "errors"

var (
	// Provider errors
	ErrUnsupportedProvider = errors.New("unsupported storage provider")
	ErrProviderNotConfigured = errors.New("storage provider not configured")
	ErrObjectNotFound = errors.New("object not found")
	ErrObjectAlreadyExists = errors.New("object already exists")

	// Backup errors
	ErrBackupNotFound = errors.New("backup not found")
	ErrBackupCorrupted = errors.New("backup is corrupted")
	ErrBackupInProgress = errors.New("backup already in progress")
	ErrInvalidBackupFormat = errors.New("invalid backup format")

	// Encryption errors
	ErrEncryptionFailed = errors.New("encryption failed")
	ErrDecryptionFailed = errors.New("decryption failed")
	ErrInvalidEncryptionKey = errors.New("invalid encryption key")

	// Compression errors
	ErrCompressionFailed = errors.New("compression failed")
	ErrDecompressionFailed = errors.New("decompression failed")

	// Metadata errors
	ErrInvalidMetadata = errors.New("invalid backup metadata")
	ErrMetadataCorrupted = errors.New("backup metadata is corrupted")

	// Restore errors
	ErrRestoreFailed = errors.New("restore operation failed")
	ErrIncompatibleBackup = errors.New("backup is incompatible with current version")

	// Scheduler errors
	ErrInvalidCronExpression = errors.New("invalid cron expression")
	ErrSchedulerNotRunning = errors.New("scheduler is not running")
)
