package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/RashikAnsar/raftkv/internal/backup"
	"github.com/RashikAnsar/raftkv/internal/backup/providers"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "kvbackup",
		Short: "RaftKV backup and restore utility",
		Long: `kvbackup is a command-line tool for managing RaftKV backups.
It supports creating backups, restoring from backups, listing backups,
and scheduling automated backups.`,
		Version: fmt.Sprintf("%s (%s)", version, commit),
	}

	// Add subcommands
	rootCmd.AddCommand(createCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(restoreCmd())
	rootCmd.AddCommand(deleteCmd())
	rootCmd.AddCommand(verifyCmd())
	rootCmd.AddCommand(scheduleCmd())

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}

// Helper function to create storage provider from config
func createProvider(cfg *backup.ProviderConfig) (backup.StorageProvider, error) {
	ctx := context.Background()
	return providers.NewStorageProvider(ctx, *cfg)
}

func createCmd() *cobra.Command {
	var (
		configPath string
		dbPath     string
		output     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new backup",
		Long:  "Create a new backup of the RaftKV database",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Create storage provider
			provider, err := createProvider(&cfg.Provider)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}

			// Convert to backup config
			backupCfg, err := cfg.ToBackupConfig()
			if err != nil {
				return fmt.Errorf("failed to convert config: %w", err)
			}

			// Create backup manager
			manager, err := backup.NewBackupManager(provider, backupCfg)
			if err != nil {
				return fmt.Errorf("failed to create backup manager: %w", err)
			}

			// Open database file
			dbFile, err := os.Open(dbPath)
			if err != nil {
				return fmt.Errorf("failed to open database file: %w", err)
			}
			defer dbFile.Close()

			// Get file info for size
			fileInfo, err := dbFile.Stat()
			if err != nil {
				return fmt.Errorf("failed to stat database file: %w", err)
			}

			log.Printf("Creating backup from %s (%d bytes)...", dbPath, fileInfo.Size())

			// Create backup (using index 0, term 0 as we don't have Raft metadata from file)
			metadata, err := manager.CreateBackup(ctx, dbFile, 0, 0)
			if err != nil {
				return fmt.Errorf("failed to create backup: %w", err)
			}

			// Print success message
			fmt.Printf("\n✓ Backup created successfully!\n\n")
			fmt.Printf("  Backup ID:       %s\n", metadata.ID)
			fmt.Printf("  Timestamp:       %s\n", metadata.Timestamp.Format(time.RFC3339))
			fmt.Printf("  Original Size:   %d bytes\n", metadata.Size)
			fmt.Printf("  Compressed Size: %d bytes\n", metadata.CompressedSize)
			if metadata.Size > 0 {
				ratio := (1.0 - float64(metadata.CompressedSize)/float64(metadata.Size)) * 100
				fmt.Printf("  Compression:     %.2f%%\n", ratio)
			}
			fmt.Printf("  Encrypted:       %v\n", metadata.Encrypted)
			fmt.Printf("  Status:          %s\n", metadata.Status)
			fmt.Printf("  Checksum:        %s\n", metadata.Checksum[:16]+"...")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().StringVarP(&dbPath, "db", "d", "", "Path to database file")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output path for backup (optional)")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("db")

	return cmd
}

func listCmd() *cobra.Command {
	var (
		configPath string
		format     string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available backups",
		Long:  "List all available backups with metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Create storage provider
			provider, err := createProvider(&cfg.Provider)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}

			// Convert to backup config
			backupCfg, err := cfg.ToBackupConfig()
			if err != nil {
				return fmt.Errorf("failed to convert config: %w", err)
			}

			// Create backup manager
			manager, err := backup.NewBackupManager(provider, backupCfg)
			if err != nil {
				return fmt.Errorf("failed to create backup manager: %w", err)
			}

			// List backups
			backups, err := manager.ListBackups(ctx)
			if err != nil {
				return fmt.Errorf("failed to list backups: %w", err)
			}

			if len(backups) == 0 {
				fmt.Println("No backups found.")
				return nil
			}

			// Print backups in table format
			fmt.Printf("\nFound %d backup(s):\n\n", len(backups))

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTIMESTAMP\tSIZE\tCOMPRESSED\tENCRYPTED\tSTATUS")
			fmt.Fprintln(w, "--\t---------\t----\t----------\t---------\t------")

			for _, b := range backups {
				fmt.Fprintf(w, "%s\t%s\t%d\t%v\t%v\t%s\n",
					b.ID[:16]+"...",
					b.Timestamp.Format("2006-01-02 15:04:05"),
					b.CompressedSize,
					b.Compressed,
					b.Encrypted,
					b.Status,
				)
			}

			w.Flush()
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().StringVarP(&format, "format", "f", "table", "Output format (table, json, yaml)")
	cmd.MarkFlagRequired("config")

	return cmd
}

func restoreCmd() *cobra.Command {
	var (
		configPath string
		backupID   string
		dbPath     string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore from backup",
		Long:  "Restore RaftKV database from a backup",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Create storage provider
			provider, err := createProvider(&cfg.Provider)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}

			// Convert to backup config
			backupCfg, err := cfg.ToBackupConfig()
			if err != nil {
				return fmt.Errorf("failed to convert config: %w", err)
			}

			// Handle 'latest' keyword
			if backupID == "latest" {
				manager, err := backup.NewBackupManager(provider, backupCfg)
				if err != nil {
					return fmt.Errorf("failed to create backup manager: %w", err)
				}

				backups, err := manager.ListBackups(ctx)
				if err != nil {
					return fmt.Errorf("failed to list backups: %w", err)
				}

				if len(backups) == 0 {
					return fmt.Errorf("no backups available")
				}

				backupID = backups[0].ID
				log.Printf("Using latest backup: %s", backupID)
			}

			// Check if output file exists
			if !force {
				if _, err := os.Stat(dbPath); err == nil {
					return fmt.Errorf("database file already exists at %s (use --force to overwrite)", dbPath)
				}
			}

			// Create directory if needed
			dir := filepath.Dir(dbPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}

			// Create compressor and encryptor for restore
			compressor, err := backup.NewCompressor(backupCfg.CompressionType, backupCfg.CompressionLevel)
			if err != nil {
				return fmt.Errorf("failed to create compressor: %w", err)
			}

			encryptor, err := backup.NewEncryptor(backupCfg.EncryptionType, backupCfg.EncryptionKey)
			if err != nil {
				return fmt.Errorf("failed to create encryptor: %w", err)
			}

			restorer := backup.NewRestoreManager(provider, compressor, encryptor)

			log.Printf("Restoring backup %s to %s...", backupID, dbPath)

			// Create output file
			outFile, err := os.Create(dbPath)
			if err != nil {
				return fmt.Errorf("failed to create output file: %w", err)
			}
			defer outFile.Close()

			// Restore backup
			err = restorer.RestoreBackup(ctx, backupID, outFile)
			if err != nil {
				os.Remove(dbPath) // Clean up on error
				return fmt.Errorf("failed to restore backup: %w", err)
			}

			fileInfo, _ := outFile.Stat()
			fmt.Printf("\n✓ Backup restored successfully!\n\n")
			fmt.Printf("  Backup ID:    %s\n", backupID)
			fmt.Printf("  Output File:  %s\n", dbPath)
			fmt.Printf("  Size:         %d bytes\n", fileInfo.Size())
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().StringVarP(&backupID, "backup", "b", "", "Backup ID to restore (or 'latest')")
	cmd.Flags().StringVarP(&dbPath, "db", "d", "", "Path to restore database to")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force restore even if database exists")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("backup")
	cmd.MarkFlagRequired("db")

	return cmd
}

func deleteCmd() *cobra.Command {
	var (
		configPath string
		backupID   string
		force      bool
	)

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a backup",
		Long:  "Delete a specific backup from storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Create storage provider
			provider, err := createProvider(&cfg.Provider)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}

			// Convert to backup config
			backupCfg, err := cfg.ToBackupConfig()
			if err != nil {
				return fmt.Errorf("failed to convert config: %w", err)
			}

			// Create backup manager
			manager, err := backup.NewBackupManager(provider, backupCfg)
			if err != nil {
				return fmt.Errorf("failed to create backup manager: %w", err)
			}

			// Confirm deletion if not forced
			if !force {
				fmt.Printf("Are you sure you want to delete backup %s? (yes/no): ", backupID)
				var response string
				fmt.Scanln(&response)
				if response != "yes" {
					fmt.Println("Deletion cancelled.")
					return nil
				}
			}

			log.Printf("Deleting backup %s...", backupID)

			// Delete backup
			err = manager.DeleteBackup(ctx, backupID)
			if err != nil {
				return fmt.Errorf("failed to delete backup: %w", err)
			}

			fmt.Printf("\n✓ Backup %s deleted successfully!\n\n", backupID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().StringVarP(&backupID, "backup", "b", "", "Backup ID to delete")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("backup")

	return cmd
}

func verifyCmd() *cobra.Command {
	var (
		configPath string
		backupID   string
	)

	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify backup integrity",
		Long:  "Verify the integrity of a backup by checking checksums",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Create storage provider
			provider, err := createProvider(&cfg.Provider)
			if err != nil {
				return fmt.Errorf("failed to create provider: %w", err)
			}

			// Convert to backup config
			backupCfg, err := cfg.ToBackupConfig()
			if err != nil {
				return fmt.Errorf("failed to convert config: %w", err)
			}

			// Create backup manager
			manager, err := backup.NewBackupManager(provider, backupCfg)
			if err != nil {
				return fmt.Errorf("failed to create backup manager: %w", err)
			}

			// Handle 'all' keyword
			if backupID == "all" {
				backups, err := manager.ListBackups(ctx)
				if err != nil {
					return fmt.Errorf("failed to list backups: %w", err)
				}

				if len(backups) == 0 {
					fmt.Println("No backups to verify.")
					return nil
				}

				fmt.Printf("Verifying %d backup(s)...\n\n", len(backups))

				failed := 0
				for i, b := range backups {
					fmt.Printf("[%d/%d] Verifying %s... ", i+1, len(backups), b.ID[:16]+"...")
					err := manager.VerifyBackup(ctx, b.ID)
					if err != nil {
						fmt.Printf("FAILED: %v\n", err)
						failed++
					} else {
						fmt.Println("OK")
					}
				}

				fmt.Printf("\n✓ Verification complete: %d passed, %d failed\n\n", len(backups)-failed, failed)
				return nil
			}

			// Verify single backup
			log.Printf("Verifying backup %s...", backupID)

			err = manager.VerifyBackup(ctx, backupID)
			if err != nil {
				return fmt.Errorf("verification failed: %w", err)
			}

			fmt.Printf("\n✓ Backup %s verified successfully!\n", backupID)
			fmt.Println("  Checksum: OK")
			fmt.Println()

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().StringVarP(&backupID, "backup", "b", "", "Backup ID to verify (or 'all')")
	cmd.MarkFlagRequired("config")
	cmd.MarkFlagRequired("backup")

	return cmd
}

func scheduleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage backup schedules",
		Long:  "Start, stop, or view backup schedules",
	}

	cmd.AddCommand(scheduleStartCmd())
	cmd.AddCommand(scheduleStopCmd())
	cmd.AddCommand(scheduleStatusCmd())

	return cmd
}

func scheduleStartCmd() *cobra.Command {
	var (
		configPath string
		daemon     bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start backup scheduler",
		Long:  "Start the automated backup scheduler",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration
			cfg, err := backup.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			if cfg.Schedule == nil || !cfg.Schedule.Enabled {
				return fmt.Errorf("scheduling is not enabled in configuration")
			}

			if daemon {
				fmt.Println("Note: Daemon mode requires a process manager (systemd, supervisord, etc.)")
				fmt.Println("For now, please run without --daemon flag or integrate with your process manager.")
				return fmt.Errorf("daemon mode not yet implemented - run without --daemon for foreground mode")
			}

			fmt.Printf("Backup scheduler would run with:\n")
			fmt.Printf("  Schedule: %s\n", cfg.Schedule.CronExpression)
			fmt.Printf("  Timezone: %s\n", cfg.Schedule.Timezone)
			fmt.Printf("  Pruning:  %v\n", cfg.Schedule.EnablePruning)
			fmt.Println("\nNote: Full scheduler implementation requires integration with the RaftKV server.")
			fmt.Println("For now, use cron or systemd timers to run 'kvbackup create' periodically.")

			return nil
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", "", "Path to backup configuration file")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "Run as daemon in background")
	cmd.MarkFlagRequired("config")

	return cmd
}

func scheduleStopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop backup scheduler",
		Long:  "Stop the running backup scheduler",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Note: Scheduler stop requires a running scheduler process.")
			fmt.Println("If using cron, disable the cron job.")
			fmt.Println("If using systemd, run: systemctl stop raftkv-backup.timer")
			return nil
		},
	}

	return cmd
}

func scheduleStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show scheduler status",
		Long:  "Show the status of the backup scheduler",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Note: Scheduler status requires a running scheduler process.")
			fmt.Println("If using cron, check: crontab -l | grep kvbackup")
			fmt.Println("If using systemd, run: systemctl status raftkv-backup.timer")
			return nil
		},
	}

	return cmd
}
