package filewriter

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
)

// BackupManager implements filewriter.BackupManager
type BackupManager struct {
	backupDir string
}

// NewBackupManager creates a new BackupManager
func NewBackupManager(baseDir string) filewriter.BackupManager {
	backupDir := filepath.Join(baseDir, ".infer", "backups")
	return &BackupManager{
		backupDir: backupDir,
	}
}

// CreateBackup creates a backup of the original file
func (b *BackupManager) CreateBackup(ctx context.Context, originalPath string) (string, error) {
	originalInfo, err := os.Stat(originalPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat original file: %w", err)
	}

	if err := os.MkdirAll(b.backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("%s.%s.backup", filepath.Base(originalPath), timestamp)
	backupPath := filepath.Join(b.backupDir, filename)

	if err := b.copyFile(originalPath, backupPath); err != nil {
		return "", fmt.Errorf("failed to create backup: %w", err)
	}

	if err := os.Chmod(backupPath, originalInfo.Mode()); err != nil {
		return backupPath, nil
	}

	return backupPath, nil
}

// RestoreBackup restores a backup to the original location
func (b *BackupManager) RestoreBackup(ctx context.Context, backupPath string, originalPath string) error {
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("backup file does not exist: %w", err)
	}

	backupInfo, err := os.Stat(backupPath)
	if err != nil {
		return fmt.Errorf("failed to stat backup file: %w", err)
	}

	targetDir := filepath.Dir(originalPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	if err := b.copyFile(backupPath, originalPath); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	if err := os.Chmod(originalPath, backupInfo.Mode()); err != nil {
		return nil
	}

	return nil
}

// CleanupBackup removes a backup file
func (b *BackupManager) CleanupBackup(backupPath string) error {
	if backupPath == "" {
		return nil
	}

	if !b.isInBackupDir(backupPath) {
		return fmt.Errorf("refusing to delete file outside backup directory: %s", backupPath)
	}

	if err := os.Remove(backupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove backup file: %w", err)
	}

	return nil
}

// copyFile copies a file from src to dst
func (b *BackupManager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	return destFile.Sync()
}

// isInBackupDir checks if a path is within the backup directory
func (b *BackupManager) isInBackupDir(path string) bool {
	absBackupDir, err := filepath.Abs(b.backupDir)
	if err != nil {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absBackupDir, absPath)
	if err != nil {
		return false
	}

	return !strings.HasPrefix(rel, "..")
}
