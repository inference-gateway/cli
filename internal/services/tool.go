package services

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// Internal result types for backwards compatibility with existing methods
type ToolResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

type FileReadResult struct {
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Size      int64  `json:"size"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Error     string `json:"error,omitempty"`
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

// LLMToolService implements ToolService with direct tool execution
type LLMToolService struct {
	config           *config.Config
	fileService      domain.FileService
	fetchService     domain.FetchService
	webSearchService domain.WebSearchService
	enabled          bool
}

// NewLLMToolService creates a new LLM tool service
func NewLLMToolService(cfg *config.Config, fileService domain.FileService, fetchService domain.FetchService, webSearchService domain.WebSearchService) *LLMToolService {
	return &LLMToolService{
		config:           cfg,
		fileService:      fileService,
		fetchService:     fetchService,
		webSearchService: webSearchService,
		enabled:          cfg.Tools.Enabled,
	}
}

func (s *LLMToolService) ListTools() []domain.ToolDefinition {
	if !s.enabled {
		return []domain.ToolDefinition{}
	}

	tools := []domain.ToolDefinition{
		{
			Name:        "Bash",
			Description: "Execute whitelisted bash commands securely",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The bash command to execute",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "Read",
			Description: "Read file content from the filesystem with optional line range",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "The path to the file to read",
					},
					"start_line": map[string]interface{}{
						"type":        "integer",
						"description": "Starting line number (1-indexed, optional)",
						"minimum":     1,
					},
					"end_line": map[string]interface{}{
						"type":        "integer",
						"description": "Ending line number (1-indexed, optional)",
						"minimum":     1,
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"file_path"},
			},
		},
		{
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
		},
	}

	if s.config.Fetch.Enabled {
		tools = append(tools, domain.ToolDefinition{
			Name:        "Fetch",
			Description: "Fetch content from whitelisted URLs or GitHub references. Supports 'github:owner/repo#123' syntax for GitHub issues/PRs.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "The URL to fetch content from, or GitHub reference (github:owner/repo#123)",
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"url"},
			},
		})
	}

	if s.config.WebSearch.Enabled {
		tools = append(tools, domain.ToolDefinition{
			Name:        "WebSearch",
			Description: "Search the web using Google or DuckDuckGo search engines",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "The search query to execute",
					},
					"engine": map[string]interface{}{
						"type":        "string",
						"description": fmt.Sprintf("The search engine to use (%s). %s is recommended for reliable results.", strings.Join(s.config.WebSearch.Engines, " or "), s.config.WebSearch.DefaultEngine),
						"enum":        s.config.WebSearch.Engines,
						"default":     s.config.WebSearch.DefaultEngine,
					},
					"limit": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of search results to return",
						"minimum":     1,
						"maximum":     50,
						"default":     s.config.WebSearch.MaxResults,
					},
					"format": map[string]interface{}{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{"query"},
			},
		})
	}

	return tools
}

func (s *LLMToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	if !s.enabled {
		return nil, fmt.Errorf("tools are not enabled")
	}

	switch name {
	case "Bash":
		return s.executeBashTool(ctx, args)
	case "Read":
		return s.executeReadTool(args)
	case "FileSearch":
		return s.executeFileSearchTool(args)
	case "Fetch":
		return s.executeFetchTool(ctx, args)
	case "WebSearch":
		return s.executeWebSearchTool(ctx, args)
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (s *LLMToolService) IsToolEnabled(name string) bool {
	if !s.enabled {
		return false
	}

	tools := s.ListTools()
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (s *LLMToolService) ValidateTool(name string, args map[string]interface{}) error {
	if !s.enabled {
		return fmt.Errorf("tools are not enabled")
	}

	if !s.IsToolEnabled(name) {
		return fmt.Errorf("tool '%s' is not available", name)
	}

	switch name {
	case "Bash":
		return s.validateBashTool(args)
	case "Read":
		return s.validateReadTool(args)
	case "FileSearch":
		return s.validateFileSearchTool(args)
	case "Fetch":
		return s.validateFetchTool(args)
	case "WebSearch":
		return s.validateWebSearchTool(args)
	default:
		return nil
	}
}

// executeBash executes a bash command with security validation
func (s *LLMToolService) executeBash(ctx context.Context, command string) (*ToolResult, error) {
	if !s.isCommandAllowed(command) {
		return nil, fmt.Errorf("command not whitelisted: %s", command)
	}

	start := time.Now()
	result := &ToolResult{
		Command: command,
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()
	result.Duration = time.Since(start).String()
	result.Output = string(output)

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitError.ExitCode()
		} else {
			result.ExitCode = -1
		}
		result.Error = err.Error()
	}

	return result, nil
}

// executeRead reads a file with optional line range
func (s *LLMToolService) executeRead(filePath string, startLine, endLine int) (*FileReadResult, error) {
	result := &FileReadResult{
		FilePath:  filePath,
		StartLine: startLine,
		EndLine:   endLine,
	}

	var content string
	var err error

	if startLine > 0 || endLine > 0 {
		content, err = s.fileService.ReadFileLines(filePath, startLine, endLine)
	} else {
		content, err = s.fileService.ReadFile(filePath)
	}

	if err != nil {
		return nil, err
	}

	result.Content = content
	result.Size = int64(len(content))

	return result, nil
}

// isCommandAllowed checks if a command is whitelisted
func (s *LLMToolService) isCommandAllowed(command string) bool {
	command = strings.TrimSpace(command)

	for _, allowed := range s.config.Tools.Whitelist.Commands {
		if command == allowed || strings.HasPrefix(command, allowed+" ") {
			return true
		}
	}

	for _, pattern := range s.config.Tools.Whitelist.Patterns {
		matched, err := regexp.MatchString(pattern, command)
		if err == nil && matched {
			return true
		}
	}

	return false
}

// NoOpToolService implements ToolService as a no-op (when tools are disabled)
type NoOpToolService struct{}

// NewNoOpToolService creates a new no-op tool service
func NewNoOpToolService() *NoOpToolService {
	return &NoOpToolService{}
}

func (s *NoOpToolService) ListTools() []domain.ToolDefinition {
	return []domain.ToolDefinition{}
}

func (s *NoOpToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	return nil, fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]interface{}) error {
	return fmt.Errorf("tools are not enabled")
}

// executeBashTool handles Bash tool execution
func (s *LLMToolService) executeBashTool(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	command, ok := args["command"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Bash",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "command parameter is required and must be a string",
		}, nil
	}

	bashResult, err := s.executeBash(ctx, command)
	success := err == nil && bashResult.ExitCode == 0

	toolData := &domain.BashToolResult{
		Command:  bashResult.Command,
		Output:   bashResult.Output,
		Error:    bashResult.Error,
		ExitCode: bashResult.ExitCode,
		Duration: bashResult.Duration,
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Bash",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// executeReadTool handles Read tool execution
func (s *LLMToolService) executeReadTool(args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	filePath, ok := args["file_path"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Read",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "file_path parameter is required and must be a string",
		}, nil
	}

	var startLine, endLine int
	if startLineFloat, ok := args["start_line"].(float64); ok {
		startLine = int(startLineFloat)
	}
	if endLineFloat, ok := args["end_line"].(float64); ok {
		endLine = int(endLineFloat)
	}

	readResult, err := s.executeRead(filePath, startLine, endLine)
	success := err == nil

	var toolData *domain.FileReadToolResult
	if readResult != nil {
		toolData = &domain.FileReadToolResult{
			FilePath:  readResult.FilePath,
			Content:   readResult.Content,
			Size:      readResult.Size,
			StartLine: readResult.StartLine,
			EndLine:   readResult.EndLine,
			Error:     readResult.Error,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Read",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// validateBashTool validates Bash tool arguments
func (s *LLMToolService) validateBashTool(args map[string]interface{}) error {
	command, ok := args["command"].(string)
	if !ok {
		return fmt.Errorf("command parameter is required and must be a string")
	}

	if !s.isCommandAllowed(command) {
		return fmt.Errorf("command not whitelisted: %s", command)
	}

	return nil
}

// validateReadTool validates Read tool arguments
func (s *LLMToolService) validateReadTool(args map[string]interface{}) error {
	filePath, ok := args["file_path"].(string)
	if !ok {
		return fmt.Errorf("file_path parameter is required and must be a string")
	}

	if err := s.fileService.ValidateFile(filePath); err != nil {
		return fmt.Errorf("file validation failed: %w", err)
	}

	return s.validateLineNumbers(args)
}

// validateLineNumbers validates start_line and end_line parameters
func (s *LLMToolService) validateLineNumbers(args map[string]interface{}) error {
	var startLine float64
	var hasStartLine bool

	if startLineFloat, ok := args["start_line"].(float64); ok {
		if startLineFloat < 1 {
			return fmt.Errorf("start_line must be >= 1")
		}
		startLine = startLineFloat
		hasStartLine = true
	}

	if endLineFloat, ok := args["end_line"].(float64); ok {
		if endLineFloat < 1 {
			return fmt.Errorf("end_line must be >= 1")
		}
		if hasStartLine && endLineFloat < startLine {
			return fmt.Errorf("end_line must be >= start_line")
		}
	}

	return nil
}

// executeFetchTool handles Fetch tool execution
func (s *LLMToolService) executeFetchTool(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !s.config.Fetch.Enabled {
		return nil, fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Fetch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "url parameter is required and must be a string",
		}, nil
	}

	fetchResult, err := s.fetchService.FetchContent(ctx, url)
	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "Fetch",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      fetchResult,
	}

	if err != nil {
		result.Error = err.Error()
	}

	return result, nil
}

// validateFetchTool validates Fetch tool arguments
func (s *LLMToolService) validateFetchTool(args map[string]interface{}) error {
	if !s.config.Fetch.Enabled {
		return fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return fmt.Errorf("url parameter is required and must be a string")
	}

	if err := s.fetchService.ValidateURL(url); err != nil {
		return fmt.Errorf("URL validation failed: %w", err)
	}

	return nil
}

// executeWebSearchTool handles WebSearch tool execution
func (s *LLMToolService) executeWebSearchTool(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !s.config.WebSearch.Enabled {
		return nil, fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "query parameter is required and must be a string",
		}, nil
	}

	engine, ok := args["engine"].(string)
	if !ok {
		engine = s.config.WebSearch.DefaultEngine
	}

	var limit int
	if limitFloat, ok := args["limit"].(float64); ok {
		limit = int(limitFloat)
	} else {
		limit = s.config.WebSearch.MaxResults
	}

	var searchResult *domain.WebSearchResponse
	var err error

	switch engine {
	case "google":
		searchResult, err = s.webSearchService.SearchGoogle(ctx, query, limit)
	case "duckduckgo":
		searchResult, err = s.webSearchService.SearchDuckDuckGo(ctx, query, limit)
	default:
		return &domain.ToolExecutionResult{
			ToolName:  "WebSearch",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Sprintf("unsupported search engine: %s", engine),
		}, nil
	}

	success := err == nil

	result := &domain.ToolExecutionResult{
		ToolName:  "WebSearch",
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

// validateWebSearchTool validates WebSearch tool arguments
func (s *LLMToolService) validateWebSearchTool(args map[string]interface{}) error {
	if !s.config.WebSearch.Enabled {
		return fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return fmt.Errorf("query parameter is required and must be a string")
	}

	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("query cannot be empty")
	}

	if engine, ok := args["engine"].(string); ok {
		validEngines := make(map[string]bool)
		for _, eng := range s.config.WebSearch.Engines {
			validEngines[eng] = true
		}
		if !validEngines[engine] {
			return fmt.Errorf("unsupported search engine: %s", engine)
		}
	}

	if limitFloat, ok := args["limit"].(float64); ok {
		limit := int(limitFloat)
		if limit < 1 || limit > 50 {
			return fmt.Errorf("limit must be between 1 and 50")
		}
	}

	return nil
}

// executeFileSearchTool handles FileSearch tool execution
func (s *LLMToolService) executeFileSearchTool(args map[string]interface{}) (*domain.ToolExecutionResult, error) {
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

	// Get optional parameters with defaults
	includeDirs := false
	if includeDirsVal, ok := args["include_dirs"].(bool); ok {
		includeDirs = includeDirsVal
	}

	caseSensitive := true
	if caseSensitiveVal, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = caseSensitiveVal
	}

	searchResult, err := s.searchFiles(pattern, includeDirs, caseSensitive)
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

// validateFileSearchTool validates FileSearch tool arguments
func (s *LLMToolService) validateFileSearchTool(args map[string]interface{}) error {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return fmt.Errorf("pattern parameter is required and must be a string")
	}

	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("pattern cannot be empty")
	}

	// Validate regex pattern
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	return nil
}

// searchFiles performs the actual file search using regex patterns
func (s *LLMToolService) searchFiles(pattern string, includeDirs bool, caseSensitive bool) (*FileSearchResult, error) {
	start := time.Now()

	// Compile regex pattern with proper flags
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

	// Walk the directory tree
	err = filepath.WalkDir(cwd, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors and continue
		}

		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		// Handle directories
		if d.IsDir() {
			return s.handleSearchDirectory(d, relPath, regex, includeDirs, &matches)
		}

		// Handle files
		if s.shouldIncludeInSearch(d, relPath, regex) {
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
func (s *LLMToolService) handleSearchDirectory(d os.DirEntry, relPath string, regex *regexp.Regexp, includeDirs bool, matches *[]FileSearchMatch) error {
	// Check depth limit (reuse same logic as FileService)
	depth := strings.Count(relPath, string(filepath.Separator))
	if depth >= 10 { // maxDepth from LocalFileService
		return filepath.SkipDir
	}

	// Check if directory should be excluded (same logic as FileService)
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

	// Check if path is excluded by configuration
	if s.isPathExcluded(relPath) {
		return filepath.SkipDir
	}

	// If includeDirs is true and directory name matches pattern, add it
	if includeDirs && regex.MatchString(d.Name()) {
		*matches = append(*matches, FileSearchMatch{
			Path:    filepath.Join(filepath.Dir(relPath), d.Name()),
			Size:    0, // Directories have size 0
			IsDir:   true,
			RelPath: relPath,
		})
	}

	return nil
}

// shouldIncludeInSearch determines if a file should be included in search results
func (s *LLMToolService) shouldIncludeInSearch(d os.DirEntry, relPath string, regex *regexp.Regexp) bool {
	if !d.Type().IsRegular() {
		return false
	}

	// Skip hidden files
	if strings.HasPrefix(d.Name(), ".") {
		return false
	}

	// Check if path is excluded by configuration
	if s.isPathExcluded(relPath) {
		return false
	}

	// Check file extension exclusions (same as FileService)
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

	// Check file size (same as FileService)
	if info, err := d.Info(); err == nil && info.Size() > 100*1024 { // 100KB limit
		return false
	}

	// Check if filename or relative path matches the regex pattern
	return regex.MatchString(d.Name()) || regex.MatchString(relPath)
}

// isPathExcluded checks if a file path should be excluded based on configuration
func (s *LLMToolService) isPathExcluded(path string) bool {
	if s.config == nil {
		return false
	}

	cleanPath := filepath.Clean(path)
	normalizedPath := filepath.ToSlash(cleanPath)

	for _, excludePattern := range s.config.Tools.ExcludePaths {
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
