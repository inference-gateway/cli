package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
)

// FileServiceImpl implements domain.FileService
type FileServiceImpl struct{}

// NewFileService creates a new file service
func NewFileService() domain.FileService {
	return &FileServiceImpl{}
}

// ListProjectFiles returns a list of all files in the current directory and subdirectories
func (s *FileServiceImpl) ListProjectFiles() ([]string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	var files []string
	err = filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return s.handleDirectory(d, path, cwd)
		}

		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		if s.shouldIncludeFile(d, relPath) {
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk directory tree: %w", err)
	}

	return files, nil
}

// handleDirectory decides whether to skip directories and handles exclusions
func (s *FileServiceImpl) handleDirectory(d os.DirEntry, path, cwd string) error {
	relPath, err := filepath.Rel(cwd, path)
	if err != nil {
		return nil
	}

	if strings.HasPrefix(d.Name(), ".") && relPath != "." && d.Name() != ".infer" {
		return filepath.SkipDir
	}

	excludeDirs := map[string]bool{
		".git":         true,
		".github":      true,
		"node_modules": true,
		"vendor":       true,
		".flox":        true,
		"dist":         true,
		"build":        true,
		"bin":          true,
		".vscode":      true,
		".idea":        true,
		"target":       true,
		"__pycache__":  true,
	}

	if excludeDirs[d.Name()] {
		return filepath.SkipDir
	}

	// Allow walking into .infer directory to find .md files, but we'll filter non-.md files later
	if d.Name() == ".infer" {
		return nil
	}

	depth := strings.Count(relPath, string(filepath.Separator))
	if depth >= 10 {
		return filepath.SkipDir
	}

	return nil
}

// shouldIncludeFile determines if a file should be included in the list
func (s *FileServiceImpl) shouldIncludeFile(d os.DirEntry, relPath string) bool {
	if !d.Type().IsRegular() {
		return false
	}

	if strings.HasPrefix(relPath, ".infer"+string(filepath.Separator)) || relPath == ".infer" {
		ext := strings.ToLower(filepath.Ext(relPath))
		if ext != ".md" {
			return false
		}
	} else if strings.HasPrefix(d.Name(), ".") {
		return false
	}

	excludeExts := map[string]bool{
		".exe": true, ".bin": true, ".dll": true, ".so": true, ".dylib": true,
		".a": true, ".o": true, ".obj": true, ".pyc": true, ".class": true,
		".jar": true, ".war": true,
		".zip": true, ".tar": true, ".gz": true, ".rar": true, ".7z": true,
		".mov": true, ".mp4": true, ".avi": true, ".mp3": true, ".wav": true,
		".ico": true, ".svg": true, ".bmp": true, ".pdf": true,
		".lock": true,
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if excludeExts[ext] {
		return false
	}

	if info, err := d.Info(); err == nil {
		sizeLimit := int64(100 * 1024)
		switch ext {
		case ".md":
			sizeLimit = int64(1024 * 1024)
		case ".png", ".jpg", ".jpeg", ".gif", ".webp":
			sizeLimit = int64(10 * 1024 * 1024)
		}
		if info.Size() > sizeLimit {
			return false
		}
	}

	return true
}

// ReadFile reads the content of a file
func (s *FileServiceImpl) ReadFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return string(content), nil
}

// ReadFileLines reads specific lines from a file
func (s *FileServiceImpl) ReadFileLines(path string, startLine, endLine int) (string, error) {
	content, err := s.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(content, "\n")
	if startLine < 1 || startLine > len(lines) {
		return "", fmt.Errorf("start line %d is out of range (1-%d)", startLine, len(lines))
	}

	if endLine < startLine || endLine > len(lines) {
		endLine = len(lines)
	}

	start := startLine - 1
	end := endLine

	return strings.Join(lines[start:end], "\n"), nil
}

// ValidateFile checks if a file path is valid and accessible
func (s *FileServiceImpl) ValidateFile(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	absPath := path
	if !filepath.IsAbs(path) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		absPath = filepath.Join(cwd, path)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", path)
		}
		return fmt.Errorf("cannot access file %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	sizeLimit := int64(50 * 1024)
	maxSizeStr := "50KB"

	switch ext {
	case ".md":
		sizeLimit = int64(1024 * 1024)
		maxSizeStr = "1MB"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		sizeLimit = int64(10 * 1024 * 1024)
		maxSizeStr = "10MB"
	}

	if info.Size() > sizeLimit {
		return fmt.Errorf("file %s is too large (%d bytes), maximum size is %s", path, info.Size(), maxSizeStr)
	}

	return nil
}

// GetFileInfo returns information about a file
func (s *FileServiceImpl) GetFileInfo(path string) (domain.FileInfo, error) {
	info, err := os.Stat(path)
	if err != nil {
		return domain.FileInfo{}, fmt.Errorf("failed to get file info for %s: %w", path, err)
	}

	return domain.FileInfo{
		Path:  path,
		Size:  info.Size(),
		IsDir: info.IsDir(),
	}, nil
}
