package filewriter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// DefaultPathValidator validates file paths for security and accessibility
type DefaultPathValidator struct {
	config *config.Config
}

// NewPathValidator creates a new DefaultPathValidator
func NewPathValidator(cfg *config.Config) *DefaultPathValidator {
	return &DefaultPathValidator{
		config: cfg,
	}
}

// Validate checks if a path is valid and secure for file operations
func (v *DefaultPathValidator) Validate(path string) error {
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

	if err := v.config.ValidatePathInSandboxWrite(absPath); err != nil {
		return err
	}

	return nil
}

// IsWritable checks if a path can be written to
func (v *DefaultPathValidator) IsWritable(path string) bool {
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
		logger.Error("failed to close temp file during writability test", "path", tempFile, "error", closeErr)
		return false
	}
	if removeErr != nil {
		logger.Error("failed to remove temp file during writability test", "path", tempFile, "error", removeErr)
		return false
	}
	return true
}

// IsInSandbox checks if a path is within configured sandbox directories
func (v *DefaultPathValidator) IsInSandbox(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	return v.config.ValidatePathInSandbox(absPath) == nil
}

// canCreatePath checks if we can create a directory path
func (v *DefaultPathValidator) canCreatePath(path string) bool {
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
				logger.Error("failed to cleanup temp directory during path creation test", "path", tempDir, "error", err)
				return false
			}
			return true
		}

		current = parent
	}

	return false
}
