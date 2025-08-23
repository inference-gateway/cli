package filewriter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/inference-gateway/cli/internal/domain/filewriter"
	"github.com/inference-gateway/cli/internal/logger"
)

// SafeFileWriter implements filewriter.FileWriter with atomic operations
type SafeFileWriter struct {
	validator     filewriter.PathValidator
	backupManager filewriter.BackupManager
}

// NewSafeFileWriter creates a new SafeFileWriter
func NewSafeFileWriter(validator filewriter.PathValidator, backupManager filewriter.BackupManager) filewriter.FileWriter {
	return &SafeFileWriter{
		validator:     validator,
		backupManager: backupManager,
	}
}

// Write performs an atomic file write operation
func (w *SafeFileWriter) Write(ctx context.Context, req filewriter.WriteRequest) (*filewriter.WriteResult, error) {
	if err := w.ValidatePath(req.Path); err != nil {
		return nil, err
	}

	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	result := &filewriter.WriteResult{
		Path: absPath,
	}

	_, err = os.Stat(absPath)
	fileExists := err == nil
	result.Created = !fileExists

	if fileExists && !req.Overwrite {
		return nil, fmt.Errorf("file already exists and overwrite is false: %s", absPath)
	}

	var backupPath string
	if req.Backup && fileExists {
		backupPath, err = w.backupManager.CreateBackup(ctx, absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create backup: %w", err)
		}
		result.BackupPath = backupPath
	}

	parentDir := filepath.Dir(absPath)
	if err := os.MkdirAll(parentDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %w", err)
	}

	if err := w.writeAtomically(absPath, req.Content); err != nil {
		if backupPath != "" {
			if restoreErr := w.backupManager.RestoreBackup(ctx, backupPath, absPath); restoreErr != nil {
				// Log restore failure and return combined error
				// TODO: Add proper logging when logger is available
				return nil, fmt.Errorf("write failed: %w, and backup restore failed: %v", err, restoreErr)
			}
		}
		return nil, err
	}

	result.BytesWritten = int64(len(req.Content))
	return result, nil
}

// ValidatePath validates if a path is safe for writing
func (w *SafeFileWriter) ValidatePath(path string) error {
	return w.validator.Validate(path)
}

// writeAtomically writes content to a file atomically using temp file + rename
func (w *SafeFileWriter) writeAtomically(targetPath, content string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(targetPath), ".tmp_"+filepath.Base(targetPath)+"_")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	tempPath := tempFile.Name()

	defer func() {
		if tempFile != nil {
			if err := tempFile.Close(); err != nil {
				logger.Error("Failed to close temp file during cleanup", "path", tempPath, "error", err)
			}
			if err := os.Remove(tempPath); err != nil {
				logger.Error("Failed to remove temp file during cleanup", "path", tempPath, "error", err)
			}
		}
	}()

	if _, err := tempFile.WriteString(content); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tempFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	tempFile = nil

	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file to target: %w", err)
	}

	return nil
}
