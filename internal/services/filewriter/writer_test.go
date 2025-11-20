package filewriter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	filewriter "github.com/inference-gateway/cli/internal/domain/filewriter"
	require "github.com/stretchr/testify/require"
)

func setupWriterTest(t *testing.T) (string, filewriter.FileWriter, context.Context) {
	tempDir := t.TempDir()

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Sandbox: config.SandboxConfig{
				Directories: []string{tempDir},
			},
		},
	}

	validator := NewPathValidator(cfg)
	backupMgr := NewBackupManager(tempDir)
	writer := NewSafeFileWriter(validator, backupMgr)
	ctx := context.Background()

	return tempDir, writer, ctx
}

func TestSafeFileWriter_Write_Success(t *testing.T) {
	tempDir, writer, ctx := setupWriterTest(t)

	t.Run("write new file", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "new_file.txt")
		content := "test content"

		req := filewriter.WriteRequest{
			Path:      testPath,
			Content:   content,
			Overwrite: true,
			Backup:    false,
		}

		result, err := writer.Write(ctx, req)
		require.NoError(t, err)
		require.True(t, result.Created)
		require.Equal(t, int64(len(content)), result.BytesWritten)

		written, err := os.ReadFile(testPath)
		require.NoError(t, err)
		require.Equal(t, content, string(written))
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "existing_file.txt")
		originalContent := "original content"
		newContent := "new content"

		require.NoError(t, os.WriteFile(testPath, []byte(originalContent), 0644))

		req := filewriter.WriteRequest{
			Path:      testPath,
			Content: newContent,
			Overwrite: true,
			Backup:    false,
		}

		result, err := writer.Write(ctx, req)
		require.NoError(t, err)
		require.False(t, result.Created)

		written, err := os.ReadFile(testPath)
		require.NoError(t, err)
		require.Equal(t, newContent, string(written))
	})

	t.Run("write with backup enabled", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "backup_test.txt")
		originalContent := "original content"
		newContent := "new content"

		require.NoError(t, os.WriteFile(testPath, []byte(originalContent), 0644))

		req := filewriter.WriteRequest{
			Path:      testPath,
			Content: newContent,
			Overwrite: true,
			Backup:    true,
		}

		result, err := writer.Write(ctx, req)
		require.NoError(t, err)
		require.False(t, result.Created)
		require.NotEmpty(t, result.BackupPath)

		written, err := os.ReadFile(testPath)
		require.NoError(t, err)
		require.Equal(t, newContent, string(written))

		backup, err := os.ReadFile(result.BackupPath)
		require.NoError(t, err)
		require.Equal(t, originalContent, string(backup))
	})

	t.Run("creates parent directories", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "nested", "deep", "directory", "file.txt")
		content := "nested file content"

		req := filewriter.WriteRequest{
			Path:      testPath,
			Content: content,
			Overwrite: true,
			Backup:    false,
		}

		result, err := writer.Write(ctx, req)
		require.NoError(t, err)
		require.True(t, result.Created)

		written, err := os.ReadFile(testPath)
		require.NoError(t, err)
		require.Equal(t, content, string(written))

		parentDir := filepath.Dir(testPath)
		stat, err := os.Stat(parentDir)
		require.NoError(t, err)
		require.True(t, stat.IsDir())
	})
}

func TestSafeFileWriter_Write_Errors(t *testing.T) {
	tempDir, writer, ctx := setupWriterTest(t)

	t.Run("error when file exists and overwrite is false", func(t *testing.T) {
		testPath := filepath.Join(tempDir, "existing_no_overwrite.txt")
		originalContent := "original content"

		require.NoError(t, os.WriteFile(testPath, []byte(originalContent), 0644))

		req := filewriter.WriteRequest{
			Path:      testPath,
			Content: "new content",
			Overwrite: false,
			Backup:    false,
		}

		result, err := writer.Write(ctx, req)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "file already exists and overwrite is false")

		written, err := os.ReadFile(testPath)
		require.NoError(t, err)
		require.Equal(t, originalContent, string(written))
	})

	t.Run("error on empty path", func(t *testing.T) {
		req := filewriter.WriteRequest{
			Path:      "",
			Content: "content",
			Overwrite: true,
		}

		result, err := writer.Write(ctx, req)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "path cannot be empty")
	})

	t.Run("error on path traversal", func(t *testing.T) {
		req := filewriter.WriteRequest{
			Path:      "../../../etc/passwd",
			Content: "malicious content",
			Overwrite: true,
		}

		result, err := writer.Write(ctx, req)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "path traversal attempts are not allowed")
	})

	t.Run("error on protected path", func(t *testing.T) {
		gitPath := filepath.Join(tempDir, ".git", "config")
		req := filewriter.WriteRequest{
			Path:      gitPath,
			Content: "malicious content",
			Overwrite: true,
		}

		result, err := writer.Write(ctx, req)
		require.Error(t, err)
		require.Nil(t, result)
		require.Contains(t, err.Error(), "path is protected")
	})

	t.Run("error on path outside sandbox", func(t *testing.T) {
		req := filewriter.WriteRequest{
			Path:      "/tmp/outside_sandbox.txt",
			Content: "content",
			Overwrite: true,
		}

		result, err := writer.Write(ctx, req)
		require.Error(t, err)
		require.Nil(t, result)
	})
}
