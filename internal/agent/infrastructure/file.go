package infrastructure

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// FileServiceImpl implements agentdomain.FileService
type FileServiceImpl struct{}

// NewFileService creates a new file service
func NewFileService() agentdomain.FileService {
	return &FileServiceImpl{}
}

// ListProjectFiles returns the files under the current directory, followed by
// the user's ~/.infer files (as "~/.infer/<path>").
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

	return append(files, s.listHomeInferFiles()...), nil
}

// homeInferSkipDirs are top-level ~/.infer runtime dirs (telemetry, logs,
// binaries, models, scratch, per-project state) that only add noise.
var homeInferSkipDirs = map[string]bool{
	"telemetry": true, "logs": true, "run": true, "bin": true,
	"models": true, "tmp": true, "projects": true,
}

// listHomeInferFiles lists ~/.infer files as "~/.infer/<rel>", which
// ValidateFile and ReadFile resolve via expandHomePath. Owner-only files
// (auth.yaml, projects.yaml) are skipped: they hold credentials and private
// state that must not be offered for inlining into a prompt.
func (s *FileServiceImpl) listHomeInferFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	root := filepath.Join(home, config.ConfigDirName)
	var files []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if rel != "." && (strings.HasPrefix(d.Name(), ".") || homeInferSkipDirs[rel]) {
				return filepath.SkipDir
			}
			return nil
		}
		if info, err := d.Info(); err != nil || info.Mode().Perm()&0o044 == 0 {
			return nil
		}
		if s.shouldIncludeFile(d, rel) {
			files = append(files, "~/"+filepath.ToSlash(filepath.Join(config.ConfigDirName, rel)))
		}
		return nil
	})
	return files
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

// shouldIncludeFile determines if a file should be included in the list.
// Under ./.infer/ only markdown context files are indexed; runtime artifacts
// (tmp scratch, artifacts) live under ~/.infer/projects/<project-slug>/ and so
// are never walked here at all.
func (s *FileServiceImpl) shouldIncludeFile(d os.DirEntry, relPath string) bool {
	if !d.Type().IsRegular() {
		return false
	}

	if strings.HasPrefix(relPath, ".infer"+string(filepath.Separator)) || relPath == ".infer" {
		if strings.ToLower(filepath.Ext(relPath)) != ".md" {
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
	content, err := os.ReadFile(expandHomePath(path))
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", path, err)
	}
	return string(content), nil
}

// ValidateFile checks if a file path is valid and accessible
func (s *FileServiceImpl) ValidateFile(path string) error {
	if path == "" {
		return fmt.Errorf("file path cannot be empty")
	}

	absPath := expandHomePath(path)
	if !filepath.IsAbs(absPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
		absPath = filepath.Join(cwd, absPath)
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
