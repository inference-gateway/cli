package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// ToolResult represents the result of a tool execution
type ToolResult struct {
	Command  string `json:"command"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
	Duration string `json:"duration"`
}

// FileReadResult represents the result of a file read operation
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

func (s *LLMToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	if !s.enabled {
		return "", fmt.Errorf("tools are not enabled")
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
		return "", fmt.Errorf("unknown tool: %s", name)
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

func (s *NoOpToolService) ExecuteTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	return "", fmt.Errorf("tools are not enabled")
}

func (s *NoOpToolService) IsToolEnabled(name string) bool {
	return false
}

func (s *NoOpToolService) ValidateTool(name string, args map[string]interface{}) error {
	return fmt.Errorf("tools are not enabled")
}

// executeBashTool handles Bash tool execution
func (s *LLMToolService) executeBashTool(ctx context.Context, args map[string]interface{}) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command parameter is required and must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	result, err := s.executeBash(ctx, command)
	if err != nil {
		return "", fmt.Errorf("bash execution failed: %w", err)
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatBashResult(result), nil
}

// executeReadTool handles Read tool execution
func (s *LLMToolService) executeReadTool(args map[string]interface{}) (string, error) {
	filePath, ok := args["file_path"].(string)
	if !ok {
		return "", fmt.Errorf("file_path parameter is required and must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	var startLine, endLine int
	if startLineFloat, ok := args["start_line"].(float64); ok {
		startLine = int(startLineFloat)
	}
	if endLineFloat, ok := args["end_line"].(float64); ok {
		endLine = int(endLineFloat)
	}

	result, err := s.executeRead(filePath, startLine, endLine)
	if err != nil {
		return "", err
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatReadResult(result), nil
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

// formatBashResult formats bash execution result for text output
func (s *LLMToolService) formatBashResult(result *ToolResult) string {
	output := fmt.Sprintf("Command: %s\n", result.Command)
	output += fmt.Sprintf("Exit Code: %d\n", result.ExitCode)
	output += fmt.Sprintf("Duration: %s\n", result.Duration)

	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}

	output += fmt.Sprintf("Output:\n%s", result.Output)
	return output
}

// formatReadResult formats read result for text output
func (s *LLMToolService) formatReadResult(result *FileReadResult) string {
	output := fmt.Sprintf("File: %s\n", result.FilePath)
	if result.StartLine > 0 {
		output += fmt.Sprintf("Lines: %d", result.StartLine)
		if result.EndLine > 0 && result.EndLine != result.StartLine {
			output += fmt.Sprintf("-%d", result.EndLine)
		}
		output += "\n"
	}
	output += fmt.Sprintf("Size: %d bytes\n", result.Size)
	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}
	output += fmt.Sprintf("Content:\n%s", result.Content)
	return output
}

// executeFetchTool handles Fetch tool execution
func (s *LLMToolService) executeFetchTool(ctx context.Context, args map[string]interface{}) (string, error) {
	if !s.config.Fetch.Enabled {
		return "", fmt.Errorf("fetch tool is not enabled")
	}

	url, ok := args["url"].(string)
	if !ok {
		return "", fmt.Errorf("url parameter is required and must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	result, err := s.fetchService.FetchContent(ctx, url)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatFetchResult(result), nil
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

// formatFetchResult formats fetch result for text output
func (s *LLMToolService) formatFetchResult(result *domain.FetchResult) string {
	output := fmt.Sprintf("URL: %s\n", result.URL)
	if result.Status > 0 {
		output += fmt.Sprintf("Status: %d\n", result.Status)
	}
	output += fmt.Sprintf("Size: %d bytes\n", result.Size)
	if result.ContentType != "" {
		output += fmt.Sprintf("Content-Type: %s\n", result.ContentType)
	}
	if result.Cached {
		output += "Source: Cache\n"
	} else {
		output += "Source: Live\n"
	}

	if len(result.Metadata) > 0 {
		output += "Metadata:\n"
		for key, value := range result.Metadata {
			output += fmt.Sprintf("  %s: %s\n", key, value)
		}
	}

	output += fmt.Sprintf("Content:\n%s", result.Content)
	return output
}

// executeWebSearchTool handles WebSearch tool execution
func (s *LLMToolService) executeWebSearchTool(ctx context.Context, args map[string]interface{}) (string, error) {
	if !s.config.WebSearch.Enabled {
		return "", fmt.Errorf("web search tool is not enabled")
	}

	query, ok := args["query"].(string)
	if !ok {
		return "", fmt.Errorf("query parameter is required and must be a string")
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

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	var result *domain.WebSearchResponse
	var err error

	switch engine {
	case "google":
		result, err = s.webSearchService.SearchGoogle(ctx, query, limit)
	case "duckduckgo":
		result, err = s.webSearchService.SearchDuckDuckGo(ctx, query, limit)
	default:
		return "", fmt.Errorf("unsupported search engine: %s", engine)
	}

	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatWebSearchResult(result), nil
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

// formatWebSearchResult formats web search result for text output
func (s *LLMToolService) formatWebSearchResult(result *domain.WebSearchResponse) string {
	output := fmt.Sprintf("Query: %s\n", result.Query)
	output += fmt.Sprintf("Engine: %s\n", result.Engine)
	output += fmt.Sprintf("Results: %d\n", result.Total)
	output += fmt.Sprintf("Time: %s\n", result.Time)

	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}

	output += "\nSearch Results:\n"
	for i, res := range result.Results {
		output += fmt.Sprintf("\n%d. %s\n", i+1, res.Title)
		output += fmt.Sprintf("   URL: %s\n", res.URL)
		output += fmt.Sprintf("   Snippet: %s\n", res.Snippet)
	}

	return output
}

// executeFileSearchTool handles FileSearch tool execution
func (s *LLMToolService) executeFileSearchTool(args map[string]interface{}) (string, error) {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return "", fmt.Errorf("pattern parameter is required and must be a string")
	}

	format, ok := args["format"].(string)
	if !ok {
		format = "text"
	}

	var includeDirs bool
	if includeFloat, ok := args["include_dirs"].(bool); ok {
		includeDirs = includeFloat
	}

	var caseSensitive = true
	if caseSensitiveVal, ok := args["case_sensitive"].(bool); ok {
		caseSensitive = caseSensitiveVal
	}

	result, err := s.executeFileSearch(pattern, includeDirs, caseSensitive)
	if err != nil {
		return "", fmt.Errorf("file search failed: %w", err)
	}

	if format == "json" {
		jsonOutput, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(jsonOutput), nil
	}

	return s.formatFileSearchResult(result), nil
}

// executeFileSearch performs the actual file search operation
func (s *LLMToolService) executeFileSearch(pattern string, includeDirs bool, caseSensitive bool) (*FileSearchResult, error) {
	start := time.Now()
	result := &FileSearchResult{
		Pattern: pattern,
		Matches: []FileSearchMatch{},
	}

	// Compile the regex pattern
	var regexFlags string
	if !caseSensitive {
		regexFlags = "(?i)"
	}
	fullPattern := regexFlags + pattern

	compiledPattern, err := regexp.Compile(fullPattern)
	if err != nil {
		result.Error = fmt.Sprintf("invalid regex pattern: %v", err)
		result.Duration = time.Since(start).String()
		return result, nil
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		result.Error = fmt.Sprintf("failed to get current directory: %v", err)
		result.Duration = time.Since(start).String()
		return result, nil
	}

	// Walk through the filesystem
	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Continue despite errors
		}

		// Get relative path
		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil
		}

		// Handle directories
		if d.IsDir() {
			return s.handleDirectoryInSearch(d, relPath, includeDirs)
		}

		// Handle files - check if we should include them
		if !s.shouldIncludeFileForSearch(d, relPath) {
			return nil
		}

		// Apply pattern matching on the relative path
		if compiledPattern.MatchString(relPath) {
			var size int64
			if info, err := d.Info(); err == nil {
				size = info.Size()
			}

			match := FileSearchMatch{
				Path:    path,
				RelPath: relPath,
				Size:    size,
				IsDir:   d.IsDir(),
			}
			result.Matches = append(result.Matches, match)
		}

		return nil
	})

	if err != nil {
		result.Error = fmt.Sprintf("failed to scan directory: %v", err)
	}

	result.Total = len(result.Matches)
	result.Duration = time.Since(start).String()

	return result, nil
}

// handleDirectoryInSearch handles directory processing during file search
func (s *LLMToolService) handleDirectoryInSearch(d fs.DirEntry, relPath string, includeDirs bool) error {
	// Skip excluded directories
	if s.shouldSkipDirectory(d.Name(), relPath) {
		return filepath.SkipDir
	}

	// If we're not including directories, continue to children
	if !includeDirs {
		return nil
	}

	return nil
}

// shouldSkipDirectory determines if a directory should be skipped during search
func (s *LLMToolService) shouldSkipDirectory(dirName, relPath string) bool {
	// Use the same exclusion logic as the file service
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

	// Check if directory name is excluded
	if excludeDirs[dirName] {
		return true
	}

	// Skip hidden directories (except root ".")
	if strings.HasPrefix(dirName, ".") && relPath != "." {
		return true
	}

	// Check depth limit
	depth := strings.Count(relPath, string(filepath.Separator))
	if depth >= 10 { // Max depth limit
		return true
	}

	return false
}

// shouldIncludeFileForSearch determines if a file should be included in search results
func (s *LLMToolService) shouldIncludeFileForSearch(d fs.DirEntry, relPath string) bool {
	// Don't include non-regular files
	if !d.Type().IsRegular() {
		return false
	}

	// Use similar exclusion logic as file service but be less restrictive for search
	excludeExts := map[string]bool{
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
	}

	ext := strings.ToLower(filepath.Ext(relPath))
	if excludeExts[ext] {
		return false
	}

	// Check if path is excluded by configuration
	if s.config != nil && s.isPathExcluded(relPath) {
		return false
	}

	return true
}

// isPathExcluded checks if a path is excluded based on configuration
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

// validateFileSearchTool validates FileSearch tool arguments
func (s *LLMToolService) validateFileSearchTool(args map[string]interface{}) error {
	pattern, ok := args["pattern"].(string)
	if !ok {
		return fmt.Errorf("pattern parameter is required and must be a string")
	}

	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("pattern cannot be empty")
	}

	// Test regex compilation
	var regexFlags string
	if caseSensitive, ok := args["case_sensitive"].(bool); ok && !caseSensitive {
		regexFlags = "(?i)"
	}
	fullPattern := regexFlags + pattern

	if _, err := regexp.Compile(fullPattern); err != nil {
		return fmt.Errorf("invalid regex pattern: %w", err)
	}

	return nil
}

// formatFileSearchResult formats file search result for text output
func (s *LLMToolService) formatFileSearchResult(result *FileSearchResult) string {
	output := fmt.Sprintf("Pattern: %s\n", result.Pattern)
	output += fmt.Sprintf("Total matches: %d\n", result.Total)
	output += fmt.Sprintf("Duration: %s\n", result.Duration)

	if result.Error != "" {
		output += fmt.Sprintf("Error: %s\n", result.Error)
	}

	if len(result.Matches) > 0 {
		output += "\nMatches:\n"
		for i, match := range result.Matches {
			fileType := "file"
			if match.IsDir {
				fileType = "dir"
			}

			sizeStr := ""
			if !match.IsDir && match.Size > 0 {
				sizeStr = fmt.Sprintf(" (%d bytes)", match.Size)
			}

			output += fmt.Sprintf("%3d. [%s] %s%s\n", i+1, fileType, match.RelPath, sizeStr)
		}
	} else {
		output += "\nNo matches found.\n"
	}

	return output
}
