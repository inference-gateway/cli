package filewriter

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackupManager_CreateBackup(t *testing.T) {
	tempDir := t.TempDir()
	backupMgr := NewBackupManager(tempDir)

	testFile := filepath.Join(tempDir, "test.txt")
	testContent := "test content"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	ctx := context.Background()

	t.Run("backup existing file", func(t *testing.T) {
		backupPath, err := backupMgr.CreateBackup(ctx, testFile)
		if err != nil {
			t.Fatalf("CreateBackup() error = %v", err)
		}

		if backupPath == "" {
			t.Error("CreateBackup() returned empty backup path")
		}

		if _, err := os.Stat(backupPath); err != nil {
			t.Errorf("Backup file does not exist: %v", err)
		}

		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("Failed to read backup file: %v", err)
		}

		if string(backupContent) != testContent {
			t.Errorf("Backup content = %q, want %q", string(backupContent), testContent)
		}

		expectedDir := filepath.Join(tempDir, ".infer", "backups")
		if !strings.HasPrefix(backupPath, expectedDir) {
			t.Errorf("Backup path %q not in expected directory %q", backupPath, expectedDir)
		}

		require.NoError(t, backupMgr.CleanupBackup(backupPath))
	})

	t.Run("backup non-existent file", func(t *testing.T) {
		nonExistentFile := filepath.Join(tempDir, "nonexistent.txt")
		backupPath, err := backupMgr.CreateBackup(ctx, nonExistentFile)
		if err != nil {
			t.Fatalf("CreateBackup() error = %v", err)
		}

		if backupPath != "" {
			t.Error("CreateBackup() should return empty path for non-existent file")
		}
	})
}

func TestBackupManager_RestoreBackup(t *testing.T) {
	tempDir := t.TempDir()
	backupMgr := NewBackupManager(tempDir)

	originalFile := filepath.Join(tempDir, "original.txt")
	originalContent := "original content"
	if err := os.WriteFile(originalFile, []byte(originalContent), 0644); err != nil {
		t.Fatalf("Failed to create original file: %v", err)
	}

	ctx := context.Background()
	backupPath, err := backupMgr.CreateBackup(ctx, originalFile)
	if err != nil {
		t.Fatalf("Failed to create backup: %v", err)
	}

	modifiedContent := "modified content"
	if err := os.WriteFile(originalFile, []byte(modifiedContent), 0644); err != nil {
		t.Fatalf("Failed to modify original file: %v", err)
	}

	t.Run("restore backup", func(t *testing.T) {
		err := backupMgr.RestoreBackup(ctx, backupPath, originalFile)
		if err != nil {
			t.Fatalf("RestoreBackup() error = %v", err)
		}

		restoredContent, err := os.ReadFile(originalFile)
		if err != nil {
			t.Fatalf("Failed to read restored file: %v", err)
		}

		if string(restoredContent) != originalContent {
			t.Errorf("Restored content = %q, want %q", string(restoredContent), originalContent)
		}
	})

	t.Run("restore to different location", func(t *testing.T) {
		newFile := filepath.Join(tempDir, "restored.txt")
		err := backupMgr.RestoreBackup(ctx, backupPath, newFile)
		if err != nil {
			t.Fatalf("RestoreBackup() error = %v", err)
		}

		newContent, err := os.ReadFile(newFile)
		if err != nil {
			t.Fatalf("Failed to read new file: %v", err)
		}

		if string(newContent) != originalContent {
			t.Errorf("New file content = %q, want %q", string(newContent), originalContent)
		}
	})

	t.Run("restore non-existent backup", func(t *testing.T) {
		nonExistentBackup := filepath.Join(tempDir, "nonexistent.backup")
		err := backupMgr.RestoreBackup(ctx, nonExistentBackup, originalFile)
		if err == nil {
			t.Error("RestoreBackup() should error for non-existent backup")
		}
	})

	require.NoError(t, backupMgr.CleanupBackup(backupPath))
}

func TestBackupManager_CleanupBackup(t *testing.T) {
	tempDir := t.TempDir()
	backupMgr := NewBackupManager(tempDir)

	backupDir := filepath.Join(tempDir, ".infer", "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("Failed to create backup directory: %v", err)
	}

	backupFile := filepath.Join(backupDir, "test.backup")
	if err := os.WriteFile(backupFile, []byte("backup content"), 0644); err != nil {
		t.Fatalf("Failed to create backup file: %v", err)
	}

	t.Run("cleanup existing backup", func(t *testing.T) {
		err := backupMgr.CleanupBackup(backupFile)
		if err != nil {
			t.Fatalf("CleanupBackup() error = %v", err)
		}

		if _, err := os.Stat(backupFile); !os.IsNotExist(err) {
			t.Error("Backup file should have been deleted")
		}
	})

	t.Run("cleanup non-existent backup", func(t *testing.T) {
		err := backupMgr.CleanupBackup(backupFile)
		if err != nil {
			t.Fatalf("CleanupBackup() should not error for non-existent file: %v", err)
		}
	})

	t.Run("cleanup empty path", func(t *testing.T) {
		err := backupMgr.CleanupBackup("")
		if err != nil {
			t.Fatalf("CleanupBackup() should not error for empty path: %v", err)
		}
	})

	t.Run("refuse to cleanup file outside backup directory", func(t *testing.T) {
		outsideFile := filepath.Join(tempDir, "outside.txt")
		if err := os.WriteFile(outsideFile, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to create outside file: %v", err)
		}

		err := backupMgr.CleanupBackup(outsideFile)
		if err == nil {
			t.Error("CleanupBackup() should refuse to delete files outside backup directory")
		}

		if _, err := os.Stat(outsideFile); os.IsNotExist(err) {
			t.Error("Outside file should not have been deleted")
		}
	})
}
