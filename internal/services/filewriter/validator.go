package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain/filewriter"
	"github.com/inference-gateway/cli/internal/logger"
)

// PathValidator implements filewriter.PathValidator
type PathValidator struct {
	config *config.Config
}

// NewPathValidator creates a new PathValidator
func NewPathValidator(cfg *config.Config) filewriter.PathValidator {
	return &PathValidator{
		config: cfg,
	}
}

// Validate checks if a path is valid and secure for file operations
func (v *PathValidator) Validate(path string) error {
	if path == "" {
		return fmt.Errorf("path cannot be empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path for '%s': %w", path, err)
	}

	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal attempts are not allowed: %s", path)
	}

	if strings.Contains(path, "\x00") {
		return fmt.Errorf("path contains null bytes: %s", path)
	}

	if err := v.config.ValidatePathInSandbox(absPath); err != nil {
		return err
	}

	if v.isProtectedPath(absPath) {
		return fmt.Errorf("path is protected and cannot be modified: %s", path)
	}

	return nil
}

// IsWritable checks if a path can be written to
func (v *PathValidator) IsWritable(path string) bool {
	if err := v.Validate(path); err != nil {
		return false
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	if _, err := os.Stat(absPath); err == nil {
		file, err := os.OpenFile(absPath, os.O_WRONLY, 0)
		if err != nil {
			return false
		}
		if err := file.Close(); err != nil {
			return false
		}
		return true
	}

	dir := filepath.Dir(absPath)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return v.canCreatePath(dir)
	}

	tempFile := filepath.Join(dir, ".write_test_"+filepath.Base(absPath))
	file, err := os.OpenFile(tempFile, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return false
	}
	closeErr := file.Close()
	removeErr := os.Remove(tempFile)

	if closeErr != nil {
		logger.Error("Failed to close temp file during writability test", "path", tempFile, "error", closeErr)
		return false
	}
	if removeErr != nil {
		logger.Error("Failed to remove temp file during writability test", "path", tempFile, "error", removeErr)
		return false
	}
	return true
}

// IsInSandbox checks if a path is within configured sandbox directories
func (v *PathValidator) IsInSandbox(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	return v.config.ValidatePathInSandbox(absPath) == nil
}

// isProtectedPath checks if a path is in the protected list
func (v *PathValidator) isProtectedPath(path string) bool {
	protectedPatterns := []string{
		".infer/",
		".git/",
		".env",
		".environment",
		"*.key",
		"*.pem",
		"id_rsa",
		"id_dsa",
		"id_ecdsa",
		"id_ed25519",
	}

	cleanPath := filepath.Clean(path)

	for _, pattern := range protectedPatterns {
		if strings.Contains(cleanPath, pattern) {
			return true
		}

		filename := filepath.Base(cleanPath)
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}

	return false
}

// canCreatePath checks if we can create a directory path
func (v *PathValidator) canCreatePath(path string) bool {
	current := path
	for {
		parent := filepath.Dir(current)
		if parent == current {
			break
		}

		if _, err := os.Stat(parent); err == nil {
			tempDir := filepath.Join(parent, ".mkdir_test_"+filepath.Base(current))
			if err := os.MkdirAll(tempDir, 0755); err != nil {
				return false
			}
			if err := os.RemoveAll(tempDir); err != nil {
				logger.Error("Failed to cleanup temp directory during path creation test", "path", tempDir, "error", err)
				return false
			}
			return true
		}

		current = parent
	}

	return false
}
