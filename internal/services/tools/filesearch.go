package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// FileSearchTool handles file searching operations using regex patterns
type FileSearchTool struct {
	config  *config.Config
	enabled bool
}

// NewFileSearchTool creates a new file search tool
func NewFileSearchTool(cfg *config.Config) *FileSearchTool {
	return &FileSearchTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *FileSearchTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "FileSearch",
		Description: "Search for files in the filesystem using regex patterns on file names and paths. Useful for finding files before reading them. Examples: 'search for config files' -> \".*config.*\\.(yaml|yml|json)$\", 'find Go source files' -> \"\\.go$\", 'locate test files' -> \".*test.*\\.go$\"",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Regex pattern to match against file paths. Examples: \"\\.go$\" (Go files), \".*config.*\" (files with 'config' in name), \"^cmd/.*\\.go$\" (Go files in cmd directory)",
				},
				"include_dirs": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether to include directories in search results (default: false)",
					"default":     false,
				},
				"case_sensitive": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the pattern matching should be case sensitive (default: true)",
					"default":     true,
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (text or json)",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

// Execute runs the file search tool with given arguments
func (t *FileSearchTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	pattern, ok := args["pattern"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "FileSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "pattern parameter is required and must be a string",
		}, nil
	}

	includeDirs := false
	if includeDirsVal, ok := args["include_dirs"].(bool); ok {
		includeDirs = includeDirsVal
	}

	caseSensitive := true
	if caseSensitiveVal, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = caseSensitiveVal
	}

	searchResult, err := t.searchFiles(pattern, includeDirs, caseSensitive)
	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "FileSearch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      searchResult,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// Validate checks if the file search tool arguments are valid
func (t *FileSearchTool) Validate(args map[string]interface{}) error {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return fmt.Errorf("pattern parameter is required and must be a string")
	}

	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("pattern cannot be empty")
	}

	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	return nil
}

// IsEnabled returns whether the file search tool is enabled
func (t *FileSearchTool) IsEnabled() bool {
	return t.enabled
}

// FileSearchResult represents the result of a file search operation
type FileSearchResult struct {
	Pattern  string            `json:"pattern"`
	Matches  []FileSearchMatch `json:"matches"`
	Total    int               `json:"total"`
	Duration string            `json:"duration"`
	Error    string            `json:"error,omitempty"`
}

// FileSearchMatch represents a single file match
type FileSearchMatch struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"is_dir"`
	RelPath string `json:"rel_path"`
}

// searchFiles performs the actual file search using regex patterns
func (t *FileSearchTool) searchFiles(pattern string, includeDirs bool, caseSensitive bool) (*FileSearchResult, error) {
	start := time.Now()

	var regex *regexp.Regexp
	var err error
	if caseSensitive {
		regex, err = regexp.Compile(pattern)
	} else {
		regex, err = regexp.Compile("(?i)" + pattern)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to compile regex: %w", err)
	}

	var matches []FileSearchMatch
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	err = filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return t.handleSearchDirectory(d, relPath, regex, includeDirs, &matches)
		}

		if t.shouldIncludeInSearch(d, relPath, regex) {
			info, err := d.Info()
			if err == nil {
				matches = append(matches, FileSearchMatch{
					Path:    path,
					Size:    info.Size(),
					IsDir:   false,
					RelPath: relPath,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to search files: %w", err)
	}

	return &FileSearchResult{
		Pattern:  pattern,
		Matches:  matches,
		Total:    len(matches),
		Duration: time.Since(start).String(),
	}, nil
}

// handleSearchDirectory handles directory processing during search
func (t *FileSearchTool) handleSearchDirectory(d os.DirEntry, relPath string, regex *regexp.Regexp, includeDirs bool, matches *[]FileSearchMatch) error {
	depth := strings.Count(relPath, string(filepath.Separator))
	if depth >= 10 {
		return filepath.SkipDir
	}

	excludeDirs := map[string]bool{
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
	}

	if excludeDirs[d.Name()] {
		return filepath.SkipDir
	}

	if strings.HasPrefix(d.Name(), ".") && relPath != "." {
		return filepath.SkipDir
	}

	if t.isPathExcluded(relPath) {
		return filepath.SkipDir
	}

	if includeDirs && regex.MatchString(d.Name()) {
		*matches = append(*matches, FileSearchMatch{
			Path:    filepath.Join(filepath.Dir(relPath), d.Name()),
			Size:    0,
			IsDir:   true,
			RelPath: relPath,
		})
	}

	return nil
}

// shouldIncludeInSearch determines if a file should be included in search results
func (t *FileSearchTool) shouldIncludeInSearch(d os.DirEntry, relPath string, regex *regexp.Regexp) bool {
	if !d.Type().IsRegular() {
		return false
	}

	if strings.HasPrefix(d.Name(), ".") {
		return false
	}

	if t.isPathExcluded(relPath) {
		return false
	}

	excludeExts := map[string]bool{
		".exe":   true, ".bin": true, ".dll": true, ".so": true, ".dylib": true,
		".a": true, ".o": true, ".obj": true, ".pyc": true, ".class": true,
		".jar": true, ".war": true, ".zip": true, ".tar": true, ".gz": true,
		".rar": true, ".7z": true, ".png": true, ".jpg": true, ".jpeg": true,
		".gif": true, ".bmp": true, ".ico": true, ".svg": true, ".pdf": true,
		".mov": true, ".mp4": true, ".avi": true, ".mp3": true, ".wav": true,
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if excludeExts[ext] {
		return false
	}

	if info, err := d.Info(); err == nil && info.Size() > 100*1024 { // 100KB limit
		return false
	}

	return regex.MatchString(d.Name()) || regex.MatchString(relPath)
}

// isPathExcluded checks if a file path should be excluded based on configuration
func (t *FileSearchTool) isPathExcluded(path string) bool {
	if t.config == nil {
		return false
	}

	cleanPath := filepath.Clean(path)
	normalizedPath := filepath.ToSlash(cleanPath)

	for _, excludePattern := range t.config.Tools.ExcludePaths {
		cleanPattern := filepath.Clean(excludePattern)
		normalizedPattern := filepath.ToSlash(cleanPattern)

		if normalizedPath == normalizedPattern {
			return true
		}

		if strings.HasSuffix(normalizedPattern, "/*") {
			dirPattern := strings.TrimSuffix(normalizedPattern, "/*")
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return true
			}
		}

		if strings.HasSuffix(normalizedPattern, "/") {
			dirPattern := strings.TrimSuffix(normalizedPattern, "/")
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return true
			}
		}

		if strings.HasPrefix(normalizedPath, normalizedPattern) {
			return true
		}
	}

	return false
}