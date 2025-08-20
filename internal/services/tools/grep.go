package tools

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// GrepTool handles search operations with ripgrep fallback to Go implementation
type GrepTool struct {
	config            *config.Config
	enabled           bool
	gitignorePatterns []string
	ripgrepPath       string
	useRipgrep        bool
}

// NewGrepTool creates a new grep tool
func NewGrepTool(cfg *config.Config) *GrepTool {
	tool := &GrepTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Grep.Enabled,
	}
	tool.loadGitignorePatterns()
	tool.detectRipgrep()
	return tool
}

// detectRipgrep checks if ripgrep is available and sets up the tool accordingly
func (t *GrepTool) detectRipgrep() {
	backend := t.config.Tools.Grep.Backend
	if backend == "" {
		backend = "auto"
	}

	switch backend {
	case "ripgrep", "rg":
		if rgPath, err := exec.LookPath("rg"); err == nil {
			t.ripgrepPath = rgPath
			t.useRipgrep = true
		} else {
			t.useRipgrep = false
		}
	case "go", "native":
		t.useRipgrep = false
	case "auto":
		if rgPath, err := exec.LookPath("rg"); err == nil {
			t.ripgrepPath = rgPath
			t.useRipgrep = true
		} else {
			t.useRipgrep = false
		}
	default:
		if rgPath, err := exec.LookPath("rg"); err == nil {
			t.ripgrepPath = rgPath
			t.useRipgrep = true
		} else {
			t.useRipgrep = false
		}
	}
}

// Definition returns the tool definition for the LLM
func (t *GrepTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Grep",
		Description: "A powerful search tool with configurable backend (ripgrep or Go implementation)\n\n  Usage:\n  - ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command. The Grep tool has been optimized for correct permissions and access.\n  - Supports full regex syntax (e.g., \"log.*Error\", \"function\\s+\\w+\")\n  - Filter files with glob parameter (e.g., \"*.js\", \"**/*.tsx\") or type parameter (e.g., \"js\", \"py\", \"rust\")\n  - Output modes: \"content\" shows matching lines, \"files_with_matches\" shows only file paths (default), \"count\" shows match counts\n  - Use Task tool for open-ended searches requiring multiple rounds\n  - Pattern syntax: When using ripgrep backend - literal braces need escaping (use `interface\\{\\}` to find `any` in Go code)\n  - Multiline matching: By default patterns match within single lines only. For cross-line patterns like `struct \\{[\\s\\S]*?field`, use `multiline: true`\n",
		Parameters: map[string]any{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"properties": map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "The regular expression pattern to search for in file contents",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "File or directory to search in (rg PATH). Defaults to current working directory.",
				},
				"glob": map[string]any{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g. \"*.js\", \"*.{ts,tsx}\") - maps to rg --glob",
				},
				"type": map[string]any{
					"type":        "string",
					"description": "File type to search (rg --type). Common types: js, py, rust, go, java, etc. More efficient than include for standard file types.",
				},
				"output_mode": map[string]any{
					"type":        "string",
					"description": "Output mode: \"content\" shows matching lines (supports -A/-B/-C context, -n line numbers), \"files_with_matches\" shows file paths (supports head_limit), \"count\" shows match counts (supports head_limit). Defaults to \"files_with_matches\".",
					"enum":        []string{"content", "files_with_matches", "count"},
					"default":     "files_with_matches",
				},
				"-i": map[string]any{
					"type":        "boolean",
					"description": "Case insensitive search (rg -i)",
				},
				"-n": map[string]any{
					"type":        "boolean",
					"description": "Show line numbers in output (rg -n). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-A": map[string]any{
					"type":        "number",
					"description": "Number of lines to show after each match (rg -A). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-B": map[string]any{
					"type":        "number",
					"description": "Number of lines to show before each match (rg -B). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-C": map[string]any{
					"type":        "number",
					"description": "Number of lines to show before and after each match (rg -C). Requires output_mode: \"content\", ignored otherwise.",
				},
				"multiline": map[string]any{
					"type":        "boolean",
					"description": "Enable multiline mode where . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false.",
					"default":     false,
				},
				"head_limit": map[string]any{
					"type":        "number",
					"description": "Limit output to first N lines/entries, equivalent to \"| head -N\". Works across all output modes: content (limits output lines), files_with_matches (limits file paths), count (limits count entries). When unspecified, shows all results from ripgrep.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

// Execute runs the grep tool with given arguments
func (t *GrepTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("grep tool is not enabled")
	}

	pattern, ok := args["pattern"].(string)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "Grep",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "pattern parameter is required and must be a string",
		}, nil
	}

	var result *GrepResult
	var err error

	if t.useRipgrep {
		result, err = t.performRipgrepSearch(ctx, pattern, args)
	} else {
		result, err = t.performGoSearch(ctx, pattern, args)
	}
	success := err == nil

	toolResult := &domain.ToolExecutionResult{
		ToolName:  "Grep",
		Arguments: args,
		Success:   success,
		Duration:  time.Since(start),
		Data:      result,
	}

	if err != nil {
		toolResult.Error = err.Error()
	}

	return toolResult, nil
}

// Validate checks if the grep tool arguments are valid
func (t *GrepTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("grep tool is not enabled")
	}

	if err := t.validatePattern(args); err != nil {
		return err
	}
	if err := t.validateOutputMode(args); err != nil {
		return err
	}
	if err := t.validateContextFlags(args); err != nil {
		return err
	}
	if err := t.validateHeadLimit(args); err != nil {
		return err
	}
	if err := t.validateBooleanFlags(args); err != nil {
		return err
	}

	return nil
}

// validatePattern validates the pattern parameter
func (t *GrepTool) validatePattern(args map[string]any) error {
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

// validateOutputMode validates the output_mode parameter
func (t *GrepTool) validateOutputMode(args map[string]any) error {
	outputMode, exists := args["output_mode"]
	if !exists {
		return nil
	}

	outputModeStr, ok := outputMode.(string)
	if !ok {
		return fmt.Errorf("output_mode must be a string")
	}

	validModes := map[string]bool{
		"content":            true,
		"files_with_matches": true,
		"count":              true,
	}
	if !validModes[outputModeStr] {
		return fmt.Errorf("invalid output_mode: %s, must be one of: content, files_with_matches, count", outputModeStr)
	}

	return nil
}

// validateContextFlags validates context flags (-A, -B, -C)
func (t *GrepTool) validateContextFlags(args map[string]any) error {
	for _, flag := range []string{"-A", "-B", "-C"} {
		value, exists := args[flag]
		if !exists {
			continue
		}

		valueFloat, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%s must be a number", flag)
		}
		if valueFloat < 0 {
			return fmt.Errorf("%s must be >= 0", flag)
		}
	}
	return nil
}

// validateHeadLimit validates the head_limit parameter
func (t *GrepTool) validateHeadLimit(args map[string]any) error {
	headLimit, exists := args["head_limit"]
	if !exists {
		return nil
	}

	headLimitFloat, ok := headLimit.(float64)
	if !ok {
		return fmt.Errorf("head_limit must be a number")
	}
	if headLimitFloat <= 0 {
		return fmt.Errorf("head_limit must be > 0")
	}

	return nil
}

// validateBooleanFlags validates boolean flags
func (t *GrepTool) validateBooleanFlags(args map[string]any) error {
	for _, flag := range []string{"-i", "-n", "multiline"} {
		value, exists := args[flag]
		if !exists {
			continue
		}

		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", flag)
		}
	}
	return nil
}

// IsEnabled returns whether the grep tool is enabled
func (t *GrepTool) IsEnabled() bool {
	return t.enabled
}

// GrepResult represents the result of a grep operation
type GrepResult struct {
	Pattern    string      `json:"pattern"`
	OutputMode string      `json:"output_mode"`
	Files      []string    `json:"files,omitempty"`
	Matches    []GrepMatch `json:"matches,omitempty"`
	Counts     []GrepCount `json:"counts,omitempty"`
	Total      int         `json:"total"`
	Truncated  bool        `json:"truncated"`
	Duration   string      `json:"duration"`
	Error      string      `json:"error,omitempty"`
}

// GrepMatch represents a single match with line content
type GrepMatch struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// GrepCount represents match count per file
type GrepCount struct {
	File  string `json:"file"`
	Count int    `json:"count"`
}

// performRipgrepSearch executes ripgrep-based search with given parameters
func (t *GrepTool) performRipgrepSearch(ctx context.Context, pattern string, args map[string]any) (*GrepResult, error) {
	start := time.Now()

	outputMode := t.getOutputMode(args)
	searchPath, err := t.getSearchPath(args)
	if err != nil {
		return nil, err
	}

	rgArgs := t.buildRipgrepArgs(outputMode, args)
	rgArgs = append(rgArgs, pattern, searchPath)

	result, err := t.executeRipgrep(ctx, rgArgs, outputMode, pattern, start)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// buildRipgrepArgs constructs the ripgrep command arguments
func (t *GrepTool) buildRipgrepArgs(outputMode string, args map[string]any) []string {
	var rgArgs []string

	rgArgs = t.addOutputModeArgs(rgArgs, outputMode, args)
	rgArgs = t.addSearchOptions(rgArgs, args)

	return rgArgs
}

// addOutputModeArgs adds output mode specific arguments
func (t *GrepTool) addOutputModeArgs(rgArgs []string, outputMode string, args map[string]any) []string {
	switch outputMode {
	case "files_with_matches":
		rgArgs = append(rgArgs, "--files-with-matches")
	case "count":
		rgArgs = append(rgArgs, "--count")
	case "content":
		rgArgs = append(rgArgs, "--with-filename")
		rgArgs = t.addContextArgs(rgArgs, args)
	}
	return rgArgs
}

// addContextArgs adds context-related arguments for content mode
func (t *GrepTool) addContextArgs(rgArgs []string, args map[string]any) []string {
	if showLineNumbers, exists := args["-n"]; exists {
		if showLineNumbersBool, ok := showLineNumbers.(bool); ok && showLineNumbersBool {
			rgArgs = append(rgArgs, "--line-number")
		}
	}

	if contextAfter, exists := args["-A"]; exists {
		if contextAfterFloat, ok := contextAfter.(float64); ok {
			rgArgs = append(rgArgs, "-A", strconv.Itoa(int(contextAfterFloat)))
		}
	}
	if contextBefore, exists := args["-B"]; exists {
		if contextBeforeFloat, ok := contextBefore.(float64); ok {
			rgArgs = append(rgArgs, "-B", strconv.Itoa(int(contextBeforeFloat)))
		}
	}
	if context, exists := args["-C"]; exists {
		if contextFloat, ok := context.(float64); ok {
			rgArgs = append(rgArgs, "-C", strconv.Itoa(int(contextFloat)))
		}
	}
	return rgArgs
}

// addSearchOptions adds general search option arguments
func (t *GrepTool) addSearchOptions(rgArgs []string, args map[string]any) []string {
	if caseInsensitive, exists := args["-i"]; exists {
		if caseInsensitiveBool, ok := caseInsensitive.(bool); ok && caseInsensitiveBool {
			rgArgs = append(rgArgs, "--ignore-case")
		}
	}

	if multiline, exists := args["multiline"]; exists {
		if multilineBool, ok := multiline.(bool); ok && multilineBool {
			rgArgs = append(rgArgs, "--multiline", "--multiline-dotall")
		}
	}

	if fileType, exists := args["type"]; exists {
		if fileTypeStr, ok := fileType.(string); ok {
			rgArgs = append(rgArgs, "--type", fileTypeStr)
		}
	}

	if glob, exists := args["glob"]; exists {
		if globStr, ok := glob.(string); ok {
			rgArgs = append(rgArgs, "--glob", globStr)
		}
	}

	if headLimit, exists := args["head_limit"]; exists {
		if headLimitFloat, ok := headLimit.(float64); ok {
			rgArgs = append(rgArgs, "--max-count", strconv.Itoa(int(headLimitFloat)))
		}
	}

	return rgArgs
}

// executeRipgrep runs the ripgrep command and processes the output
func (t *GrepTool) executeRipgrep(ctx context.Context, rgArgs []string, outputMode, pattern string, start time.Time) (*GrepResult, error) {
	cmd := exec.CommandContext(ctx, t.ripgrepPath, rgArgs...)
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && exitError.ExitCode() == 1 {
			return &GrepResult{
				Pattern:    pattern,
				OutputMode: outputMode,
				Files:      []string{},
				Matches:    []GrepMatch{},
				Counts:     []GrepCount{},
				Total:      0,
				Truncated:  false,
				Duration:   time.Since(start).String(),
			}, nil
		}
		return nil, fmt.Errorf("ripgrep execution failed: %w", err)
	}

	result := t.parseRipgrepOutput(string(output), outputMode, pattern)
	result.Duration = time.Since(start).String()
	return result, nil
}

// parseRipgrepOutput parses ripgrep output into GrepResult
func (t *GrepTool) parseRipgrepOutput(output, outputMode, pattern string) *GrepResult {
	result := &GrepResult{
		Pattern:    pattern,
		OutputMode: outputMode,
		Files:      []string{},
		Matches:    []GrepMatch{},
		Counts:     []GrepCount{},
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return result
	}

	switch outputMode {
	case "files_with_matches":
		result.Files = lines
		result.Total = len(lines)
	case "count":
		for _, line := range lines {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				count, err := strconv.Atoi(parts[1])
				if err == nil {
					result.Counts = append(result.Counts, GrepCount{
						File:  parts[0],
						Count: count,
					})
				}
			}
		}
		result.Total = len(result.Counts)
	case "content":
		for _, line := range lines {
			parts := strings.SplitN(line, ":", 3)
			if len(parts) >= 3 {
				lineNum, err := strconv.Atoi(parts[1])
				if err != nil {
					lineNum = 0
				}
				result.Matches = append(result.Matches, GrepMatch{
					File: parts[0],
					Line: lineNum,
					Text: parts[2],
				})
			} else if len(parts) == 2 {
				result.Matches = append(result.Matches, GrepMatch{
					File: parts[0],
					Line: 0,
					Text: parts[1],
				})
			}
		}
		result.Total = len(result.Matches)
	}

	return result
}

// performGoSearch executes Go-based search with given parameters
func (t *GrepTool) performGoSearch(ctx context.Context, pattern string, args map[string]any) (*GrepResult, error) {
	start := time.Now()

	outputMode := t.getOutputMode(args)
	searchPath, err := t.getSearchPath(args)
	if err != nil {
		return nil, err
	}

	regexFlags := t.getRegexFlags(args)
	regex, err := regexp.Compile(regexFlags + pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	opts := t.buildSearchOptions(args, outputMode)

	result, err := t.searchFiles(ctx, regex, searchPath, opts, outputMode)
	if err != nil {
		return nil, err
	}

	result.Pattern = pattern
	result.OutputMode = outputMode
	result.Duration = time.Since(start).String()

	return result, nil
}

// SearchOptions holds configuration for the search operation
type SearchOptions struct {
	CaseInsensitive bool
	ShowLineNumbers bool
	ContextBefore   int
	ContextAfter    int
	Multiline       bool
	GlobPattern     string
	FileType        string
	HeadLimit       int
}

// getRegexFlags returns regex flags based on arguments
func (t *GrepTool) getRegexFlags(args map[string]any) string {
	var flags string
	if caseInsensitive, exists := args["-i"]; exists {
		if caseInsensitiveBool, ok := caseInsensitive.(bool); ok && caseInsensitiveBool {
			flags += "(?i)"
		}
	}
	return flags
}

// buildSearchOptions builds search options from arguments
func (t *GrepTool) buildSearchOptions(args map[string]any, outputMode string) *SearchOptions {
	opts := &SearchOptions{}

	if caseInsensitive, exists := args["-i"]; exists {
		if caseInsensitiveBool, ok := caseInsensitive.(bool); ok {
			opts.CaseInsensitive = caseInsensitiveBool
		}
	}

	if outputMode == "content" {
		if showLineNumbers, exists := args["-n"]; exists {
			if showLineNumbersBool, ok := showLineNumbers.(bool); ok {
				opts.ShowLineNumbers = showLineNumbersBool
			}
		}
	}

	if outputMode == "content" {
		t.setContextOptions(args, opts)
	}

	if multiline, exists := args["multiline"]; exists {
		if multilineBool, ok := multiline.(bool); ok {
			opts.Multiline = multilineBool
		}
	}

	if glob, exists := args["glob"]; exists {
		if globStr, ok := glob.(string); ok {
			opts.GlobPattern = globStr
		}
	}

	if fileType, exists := args["type"]; exists {
		if fileTypeStr, ok := fileType.(string); ok {
			opts.FileType = fileTypeStr
		}
	}

	if headLimit, exists := args["head_limit"]; exists {
		if headLimitFloat, ok := headLimit.(float64); ok {
			opts.HeadLimit = int(headLimitFloat)
		}
	}

	return opts
}

// setContextOptions sets context line options for content mode
func (t *GrepTool) setContextOptions(args map[string]any, opts *SearchOptions) {
	if contextAfter, exists := args["-A"]; exists {
		if contextAfterFloat, ok := contextAfter.(float64); ok {
			opts.ContextAfter = int(contextAfterFloat)
		}
	}
	if contextBefore, exists := args["-B"]; exists {
		if contextBeforeFloat, ok := contextBefore.(float64); ok {
			opts.ContextBefore = int(contextBeforeFloat)
		}
	}
	if context, exists := args["-C"]; exists {
		if contextFloat, ok := context.(float64); ok {
			opts.ContextBefore = int(contextFloat)
			opts.ContextAfter = int(contextFloat)
		}
	}
}

// getOutputMode extracts output mode from arguments
func (t *GrepTool) getOutputMode(args map[string]any) string {
	outputMode := "files_with_matches"
	if mode, exists := args["output_mode"]; exists {
		if modeStr, ok := mode.(string); ok {
			outputMode = modeStr
		}
	}
	return outputMode
}

// searchFiles performs the actual file search
func (t *GrepTool) searchFiles(ctx context.Context, regex *regexp.Regexp, searchPath string, opts *SearchOptions, outputMode string) (*GrepResult, error) {
	result := &GrepResult{
		Files:   []string{},
		Matches: []GrepMatch{},
		Counts:  []GrepCount{},
	}

	count := 0
	err := filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if d.IsDir() {
			return nil
		}

		if t.isPathExcluded(path) {
			return nil
		}

		if !t.matchesFileFilters(path, opts) {
			return nil
		}

		if opts.HeadLimit > 0 && count >= opts.HeadLimit {
			result.Truncated = true
			return fs.SkipAll
		}

		matches, fileCount, err := t.searchFile(path, regex, opts, outputMode)
		if err != nil {
			return nil
		}

		switch outputMode {
		case "files_with_matches":
			if len(matches) > 0 || fileCount > 0 {
				result.Files = append(result.Files, path)
				count++
			}
		case "count":
			if fileCount > 0 {
				result.Counts = append(result.Counts, GrepCount{
					File:  path,
					Count: fileCount,
				})
				count++
			}
		case "content":
			for _, match := range matches {
				if opts.HeadLimit > 0 && count >= opts.HeadLimit {
					result.Truncated = true
					return fs.SkipAll
				}
				result.Matches = append(result.Matches, match)
				count++
			}
		}

		return nil
	})

	if err != nil && err != fs.SkipAll {
		return nil, err
	}

	result.Total = count
	return result, nil
}

// getSearchPath extracts and validates the search path
func (t *GrepTool) getSearchPath(args map[string]any) (string, error) {
	searchPath := "."
	if path, exists := args["path"]; exists {
		if pathStr, ok := path.(string); ok && pathStr != "" {
			searchPath = pathStr
		}
	}

	if t.isPathExcluded(searchPath) {
		return "", fmt.Errorf("access to path '%s' is not allowed", searchPath)
	}

	return searchPath, nil
}

// matchesFileFilters checks if a file matches the specified filters
func (t *GrepTool) matchesFileFilters(filePath string, opts *SearchOptions) bool {
	if opts.GlobPattern != "" {
		matched, err := filepath.Match(opts.GlobPattern, filepath.Base(filePath))
		if err != nil || !matched {
			return false
		}
	}

	if opts.FileType != "" {
		if !t.matchesFileType(filePath, opts.FileType) {
			return false
		}
	}

	return true
}

// matchesFileType checks if a file matches the specified type
func (t *GrepTool) matchesFileType(filePath, fileType string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))

	typeMap := map[string][]string{
		"go":    {".go"},
		"js":    {".js", ".jsx"},
		"ts":    {".ts", ".tsx"},
		"py":    {".py", ".pyw"},
		"java":  {".java"},
		"cpp":   {".cpp", ".cc", ".cxx", ".c++", ".h", ".hpp"},
		"c":     {".c", ".h"},
		"rust":  {".rs"},
		"php":   {".php"},
		"rb":    {".rb"},
		"cs":    {".cs"},
		"swift": {".swift"},
		"kt":    {".kt"},
		"scala": {".scala"},
		"html":  {".html", ".htm"},
		"css":   {".css"},
		"json":  {".json"},
		"xml":   {".xml"},
		"yaml":  {".yaml", ".yml"},
		"md":    {".md", ".markdown"},
		"txt":   {".txt"},
		"sh":    {".sh", ".bash", ".zsh"},
		"sql":   {".sql"},
	}

	if extensions, exists := typeMap[fileType]; exists {
		for _, validExt := range extensions {
			if ext == validExt {
				return true
			}
		}
	}

	return false
}

// searchFile searches for regex matches in a single file
func (t *GrepTool) searchFile(filePath string, regex *regexp.Regexp, opts *SearchOptions, outputMode string) ([]GrepMatch, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		_ = file.Close()
	}()

	var matches []GrepMatch
	var totalMatches int

	if opts.Multiline {
		matches, totalMatches = t.searchMultiline(filePath, regex, outputMode)
	} else {
		var err error
		matches, totalMatches, err = t.searchLineByLine(file, filePath, regex, opts, outputMode)
		if err != nil {
			return nil, 0, err
		}
	}

	return matches, totalMatches, nil
}

// searchMultiline handles multiline search mode
func (t *GrepTool) searchMultiline(filePath string, regex *regexp.Regexp, outputMode string) ([]GrepMatch, int) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, 0
	}

	allMatches := regex.FindAllString(string(content), -1)
	totalMatches := len(allMatches)

	var matches []GrepMatch
	if outputMode == "content" && len(allMatches) > 0 {
		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if regex.MatchString(line) {
				matches = append(matches, GrepMatch{
					File: filePath,
					Line: 0,
					Text: line,
				})
			}
		}
	}

	return matches, totalMatches
}

// searchLineByLine handles line-by-line search mode
func (t *GrepTool) searchLineByLine(file *os.File, filePath string, regex *regexp.Regexp, opts *SearchOptions, outputMode string) ([]GrepMatch, int, error) {
	scanner := bufio.NewScanner(file)
	lineNum := 1
	var contextLines []string
	var matches []GrepMatch
	var totalMatches int

	for scanner.Scan() {
		line := scanner.Text()

		if regex.MatchString(line) {
			totalMatches++

			if outputMode == "content" {
				match := GrepMatch{
					File: filePath,
					Line: lineNum,
					Text: line,
				}

				if opts.ContextBefore > 0 || opts.ContextAfter > 0 {
					match = t.addContextToMatch(match, contextLines, scanner, opts)
				}

				matches = append(matches, match)
			}
		}

		if opts.ContextBefore > 0 {
			contextLines = append(contextLines, line)
			if len(contextLines) > opts.ContextBefore {
				contextLines = contextLines[1:]
			}
		}

		lineNum++
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	return matches, totalMatches, nil
}

// addContextToMatch adds context lines to a match (simplified implementation)
func (t *GrepTool) addContextToMatch(match GrepMatch, contextLines []string, scanner *bufio.Scanner, opts *SearchOptions) GrepMatch {
	return match
}

// isPathExcluded checks if a file path should be excluded based on configuration
func (t *GrepTool) isPathExcluded(path string) bool {
	if t.config == nil {
		return false
	}

	if t.matchesGitignorePattern(path) {
		return true
	}

	// Check if path is outside sandbox directories
	if err := t.config.ValidatePathInSandbox(path); err != nil {
		return true
	}

	return false
}

// loadGitignorePatterns reads and caches gitignore patterns from current directory and subdirectories
func (t *GrepTool) loadGitignorePatterns() {
	var patterns []string

	defaultPatterns := []string{
		"node_modules",
		".git",
		".infer",
		".DS_Store",
		"*.log",
		"dist",
		"build",
		"target",
		".cache",
		"coverage",
		".nyc_output",
		"*.tmp",
		"*.temp",
		".env",
		".vscode",
		".idea",
		".flox",
		"secret",
	}

	patterns = append(patterns, defaultPatterns...)

	t.gitignorePatterns = patterns
}

// matchesGitignorePattern checks if a path matches any gitignore pattern
func (t *GrepTool) matchesGitignorePattern(path string) bool {
	normalizedPath := filepath.ToSlash(filepath.Clean(path))

	for _, pattern := range t.gitignorePatterns {
		if strings.Contains(normalizedPath, pattern) {
			return true
		}

		if strings.Contains(pattern, "*") {
			matched, err := filepath.Match(pattern, filepath.Base(normalizedPath))
			if err == nil && matched {
				return true
			}
		}

		if strings.HasSuffix(pattern, "/") {
			dirPattern := strings.TrimSuffix(pattern, "/")
			if strings.HasPrefix(normalizedPath, dirPattern+"/") || normalizedPath == dirPattern {
				return true
			}
		} else {
			if normalizedPath == pattern || strings.HasPrefix(normalizedPath, pattern+"/") {
				return true
			}
		}
	}

	return false
}
