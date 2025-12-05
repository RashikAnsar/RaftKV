package backup

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/robfig/cron/v3"
)

// BackupScheduler manages scheduled backups
type BackupScheduler struct {
	manager      *BackupManager
	cron         *cron.Cron
	dataProvider DataProvider
	mu           sync.RWMutex
	running      bool
	scheduleID   cron.EntryID
}

// DataProvider provides data to backup
type DataProvider interface {
	// GetBackupData returns a reader for the data to backup and the current Raft state
	GetBackupData(ctx context.Context) (io.Reader, uint64, uint64, error) // reader, raftIndex, raftTerm, error
}

// ScheduleConfig contains scheduler configuration
type ScheduleConfig struct {
	CronExpression string
	Timezone       string
	EnablePruning  bool
	PruneAfterBackup bool
}

// NewBackupScheduler creates a new backup scheduler
func NewBackupScheduler(manager *BackupManager, dataProvider DataProvider) *BackupScheduler {
	return &BackupScheduler{
		manager:      manager,
		cron:         cron.New(cron.WithSeconds()),
		dataProvider: dataProvider,
		running:      false,
	}
}

// Start starts the scheduler with the given schedule
func (s *BackupScheduler) Start(config ScheduleConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scheduler is already running")
	}

	if config.CronExpression == "" {
		return ErrInvalidCronExpression
	}

	// Add backup job
	id, err := s.cron.AddFunc(config.CronExpression, func() {
		ctx := context.Background()
		if err := s.performBackup(ctx, config); err != nil {
			log.Printf("Scheduled backup failed: %v", err)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to schedule backup: %w", err)
	}

	s.scheduleID = id
	s.cron.Start()
	s.running = true

	log.Printf("Backup scheduler started with cron expression: %s", config.CronExpression)
	return nil
}

// Stop stops the scheduler
func (s *BackupScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return ErrSchedulerNotRunning
	}

	s.cron.Stop()
	s.running = false

	log.Println("Backup scheduler stopped")
	return nil
}

// TriggerBackup manually triggers a backup
func (s *BackupScheduler) TriggerBackup(ctx context.Context) (*BackupMetadata, error) {
	// Get data to backup
	reader, raftIndex, raftTerm, err := s.dataProvider.GetBackupData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup data: %w", err)
	}

	// Create backup
	metadata, err := s.manager.CreateBackup(ctx, reader, raftIndex, raftTerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}

	log.Printf("Manual backup created: %s (Raft index: %d, term: %d)", metadata.ID, raftIndex, raftTerm)
	return metadata, nil
}

// IsRunning returns whether the scheduler is running
func (s *BackupScheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetNextRun returns the next scheduled backup time
func (s *BackupScheduler) GetNextRun() *cron.Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.running {
		return nil
	}

	entry := s.cron.Entry(s.scheduleID)
	return &entry
}

// performBackup executes a scheduled backup
func (s *BackupScheduler) performBackup(ctx context.Context, config ScheduleConfig) error {
	log.Println("Starting scheduled backup...")

	// Get data to backup
	reader, raftIndex, raftTerm, err := s.dataProvider.GetBackupData(ctx)
	if err != nil {
		return fmt.Errorf("failed to get backup data: %w", err)
	}

	// Create backup
	metadata, err := s.manager.CreateBackup(ctx, reader, raftIndex, raftTerm)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	log.Printf("Scheduled backup completed: %s (Raft index: %d, term: %d, size: %d bytes)",
		metadata.ID, raftIndex, raftTerm, metadata.Size)

	// Prune old backups if enabled
	if config.EnablePruning && config.PruneAfterBackup {
		if err := s.pruneBackups(ctx); err != nil {
			log.Printf("Failed to prune backups: %v", err)
			// Don't return error, backup was successful
		}
	}

	return nil
}

// pruneBackups removes old backups
func (s *BackupScheduler) pruneBackups(ctx context.Context) error {
	// Prune by retention days
	deletedByAge, err := s.manager.PruneOldBackups(ctx)
	if err != nil {
		return fmt.Errorf("failed to prune old backups: %w", err)
	}

	if deletedByAge > 0 {
		log.Printf("Pruned %d old backups", deletedByAge)
	}

	// Prune by max count
	deletedByCount, err := s.manager.PruneExcessBackups(ctx)
	if err != nil {
		return fmt.Errorf("failed to prune excess backups: %w", err)
	}

	if deletedByCount > 0 {
		log.Printf("Pruned %d excess backups", deletedByCount)
	}

	return nil
}

// UpdateSchedule updates the backup schedule
func (s *BackupScheduler) UpdateSchedule(config ScheduleConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return ErrSchedulerNotRunning
	}

	// Remove existing schedule
	s.cron.Remove(s.scheduleID)

	// Add new schedule
	id, err := s.cron.AddFunc(config.CronExpression, func() {
		ctx := context.Background()
		if err := s.performBackup(ctx, config); err != nil {
			log.Printf("Scheduled backup failed: %v", err)
		}
	})

	if err != nil {
		return fmt.Errorf("failed to update schedule: %w", err)
	}

	s.scheduleID = id
	log.Printf("Backup schedule updated: %s", config.CronExpression)
	return nil
}
