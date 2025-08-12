package services

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/inference-gateway/cli/internal/domain"
)

// LocalFileService implements FileService using local filesystem operations
type LocalFileService struct {
	excludeDirs map[string]bool
	excludeExts map[string]bool
	maxFileSize int64
	maxDepth    int
}

// NewLocalFileService creates a new local file service
func NewLocalFileService() *LocalFileService {
	return &LocalFileService{
		excludeDirs: map[string]bool{
			".git":         true,
			".github":      true,
			"node_modules": true,
			".infer":       true,
			"vendor":       true,
			".flox":        true,
			"dist":         true,
			"build":        true,
			"bin":          true,
			".vscode":      true,
			".idea":        true,
		},
		excludeExts: map[string]bool{
			".exe":   true,
			".bin":   true,
			".dll":   true,
			".so":    true,
			".dylib": true,
			".a":     true,
			".o":     true,
			".obj":   true,
			".pyc":   true,
			".class": true,
			".jar":   true,
			".war":   true,
			".zip":   true,
			".tar":   true,
			".gz":    true,
			".rar":   true,
			".7z":    true,
			".png":   true,
			".jpg":   true,
			".jpeg":  true,
			".gif":   true,
			".bmp":   true,
			".ico":   true,
			".svg":   true,
			".pdf":   true,
			".mov":   true,
			".mp4":   true,
			".avi":   true,
			".mp3":   true,
			".wav":   true,
		},
		maxFileSize: 100 * 1024, // 100KB max file size
		maxDepth:    10,         // Maximum directory depth
	}
}

func (s *LocalFileService) ListProjectFiles() ([]string, error) {
	var files []string

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip errors but continue walking
			return nil
		}

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		depth := strings.Count(relPath, string(filepath.Separator))

		if d.IsDir() && depth >= s.maxDepth {
			return filepath.SkipDir
		}

		if d.IsDir() {
			if s.excludeDirs[d.Name()] {
				return filepath.SkipDir
			}

			if strings.HasPrefix(d.Name(), ".") && relPath != "." {
				return filepath.SkipDir
			}

			return nil
		}

		if !d.Type().IsRegular() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(relPath))
		if s.excludeExts[ext] {
			return nil
		}

		if info, err := d.Info(); err == nil && info.Size() > s.maxFileSize {
			return nil
		}

		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}

		files = append(files, relPath)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

func (s *LocalFileService) ReadFile(path string) (string, error) {
	if err := s.ValidateFile(path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

func (s *LocalFileService) ValidateFile(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", path)
	}
	if err != nil {
		return fmt.Errorf("failed to access file: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file: %s", path)
	}

	if info.Size() > s.maxFileSize {
		return fmt.Errorf("file too large (max %d bytes): %s", s.maxFileSize, path)
	}

	// Check if file extension is excluded
	ext := strings.ToLower(filepath.Ext(path))
	if s.excludeExts[ext] {
		return fmt.Errorf("file type not supported: %s", ext)
	}

	return nil
}

func (s *LocalFileService) GetFileInfo(path string) (domain.FileInfo, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return domain.FileInfo{}, fmt.Errorf("failed to resolve file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return domain.FileInfo{}, fmt.Errorf("failed to get file info: %w", err)
	}

	return domain.FileInfo{
		Path:  path,
		Size:  info.Size(),
		IsDir: info.IsDir(),
	}, nil
}
