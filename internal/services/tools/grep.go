package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// GrepTool handles ripgrep-powered search operations
type GrepTool struct {
	config  *config.Config
	enabled bool
}

// NewGrepTool creates a new grep tool
func NewGrepTool(cfg *config.Config) *GrepTool {
	return &GrepTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.Grep.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *GrepTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "Grep",
		Description: "A powerful search tool built on ripgrep\n\n  Usage:\n  - ALWAYS use Grep for search tasks. NEVER invoke `grep` or `rg` as a Bash command. The Grep tool has been optimized for correct permissions and access.\n  - Supports full regex syntax (e.g., \"log.*Error\", \"function\\s+\\w+\")\n  - Filter files with glob parameter (e.g., \"*.js\", \"**/*.tsx\") or type parameter (e.g., \"js\", \"py\", \"rust\")\n  - Output modes: \"content\" shows matching lines, \"files_with_matches\" shows only file paths (default), \"count\" shows match counts\n  - Use Task tool for open-ended searches requiring multiple rounds\n  - Pattern syntax: Uses ripgrep (not grep) - literal braces need escaping (use `interface\\{\\}` to find `interface{}` in Go code)\n  - Multiline matching: By default patterns match within single lines only. For cross-line patterns like `struct \\{[\\s\\S]*?field`, use `multiline: true`\n",
		Parameters: map[string]interface{}{
			"$schema": "http://json-schema.org/draft-07/schema#",
			"type":    "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "The regular expression pattern to search for in file contents",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "File or directory to search in (rg PATH). Defaults to current working directory.",
				},
				"glob": map[string]interface{}{
					"type":        "string",
					"description": "Glob pattern to filter files (e.g. \"*.js\", \"*.{ts,tsx}\") - maps to rg --glob",
				},
				"type": map[string]interface{}{
					"type":        "string",
					"description": "File type to search (rg --type). Common types: js, py, rust, go, java, etc. More efficient than include for standard file types.",
				},
				"output_mode": map[string]interface{}{
					"type":        "string",
					"description": "Output mode: \"content\" shows matching lines (supports -A/-B/-C context, -n line numbers), \"files_with_matches\" shows file paths (supports head_limit), \"count\" shows match counts (supports head_limit). Defaults to \"files_with_matches\".",
					"enum":        []string{"content", "files_with_matches", "count"},
					"default":     "files_with_matches",
				},
				"-i": map[string]interface{}{
					"type":        "boolean",
					"description": "Case insensitive search (rg -i)",
				},
				"-n": map[string]interface{}{
					"type":        "boolean",
					"description": "Show line numbers in output (rg -n). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-A": map[string]interface{}{
					"type":        "number",
					"description": "Number of lines to show after each match (rg -A). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-B": map[string]interface{}{
					"type":        "number",
					"description": "Number of lines to show before each match (rg -B). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-C": map[string]interface{}{
					"type":        "number",
					"description": "Number of lines to show before and after each match (rg -C). Requires output_mode: \"content\", ignored otherwise.",
				},
				"multiline": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable multiline mode where . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false.",
					"default":     false,
				},
				"head_limit": map[string]interface{}{
					"type":        "number",
					"description": "Limit output to first N lines/entries, equivalent to \"| head -N\". Works across all output modes: content (limits output lines), files_with_matches (limits file paths), count (limits count entries). When unspecified, shows all results from ripgrep.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

// Execute runs the grep tool with given arguments
func (t *GrepTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
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

	result, err := t.performRipgrep(ctx, pattern, args)
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
func (t *GrepTool) Validate(args map[string]interface{}) error {
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
func (t *GrepTool) validatePattern(args map[string]interface{}) error {
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

// validateOutputMode validates the output_mode parameter
func (t *GrepTool) validateOutputMode(args map[string]interface{}) error {
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
func (t *GrepTool) validateContextFlags(args map[string]interface{}) error {
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
func (t *GrepTool) validateHeadLimit(args map[string]interface{}) error {
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
func (t *GrepTool) validateBooleanFlags(args map[string]interface{}) error {
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

// performRipgrep executes ripgrep with given parameters
func (t *GrepTool) performRipgrep(ctx context.Context, pattern string, args map[string]interface{}) (*GrepResult, error) {
	start := time.Now()

	// Build ripgrep command
	cmd, outputMode, err := t.buildRipgrepCommand(pattern, args)
	if err != nil {
		return nil, err
	}

	// Execute ripgrep command
	output, err := t.executeRipgrep(ctx, cmd)
	if err != nil {
		// ripgrep returns non-zero exit code when no matches found, which is not an error
		if t.isNoMatchesError(err) {
			return t.createEmptyResult(pattern, outputMode, start), nil
		}
		return nil, fmt.Errorf("ripgrep execution failed: %w", err)
	}

	// Parse JSON output and build result
	result, err := t.parseRipgrepOutput(string(output), outputMode, args)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ripgrep output: %w", err)
	}

	result.Pattern = pattern
	result.OutputMode = outputMode
	result.Duration = time.Since(start).String()

	return result, nil
}

// buildRipgrepCommand builds the ripgrep command from arguments
func (t *GrepTool) buildRipgrepCommand(pattern string, args map[string]interface{}) ([]string, string, error) {
	cmd := []string{"rg", pattern}

	// Get output mode
	outputMode := t.getOutputMode(args)
	cmd = t.addOutputModeFlags(cmd, outputMode)

	// Add flags based on arguments
	cmd = t.addBooleanFlags(cmd, args, outputMode)
	cmd = t.addContextFlags(cmd, args, outputMode)
	cmd = t.addFilterFlags(cmd, args)

	// Add JSON output and path
	cmd = append(cmd, "--json")
	searchPath, err := t.getSearchPath(args)
	if err != nil {
		return nil, "", err
	}
	cmd = append(cmd, searchPath)

	return cmd, outputMode, nil
}

// getOutputMode extracts output mode from arguments
func (t *GrepTool) getOutputMode(args map[string]interface{}) string {
	outputMode := "files_with_matches"
	if mode, exists := args["output_mode"]; exists {
		if modeStr, ok := mode.(string); ok {
			outputMode = modeStr
		}
	}
	return outputMode
}

// addOutputModeFlags adds output mode specific flags
func (t *GrepTool) addOutputModeFlags(cmd []string, outputMode string) []string {
	switch outputMode {
	case "files_with_matches":
		cmd = append(cmd, "-l")
	case "count":
		cmd = append(cmd, "-c")
	case "content":
		// Default ripgrep behavior shows content
	}
	return cmd
}

// addBooleanFlags adds boolean flags to the command
func (t *GrepTool) addBooleanFlags(cmd []string, args map[string]interface{}, outputMode string) []string {
	// Add case insensitive flag
	if caseInsensitive, exists := args["-i"]; exists {
		if caseInsensitiveBool, ok := caseInsensitive.(bool); ok && caseInsensitiveBool {
			cmd = append(cmd, "-i")
		}
	}

	// Add line numbers (only for content mode)
	if outputMode == "content" {
		if showLineNumbers, exists := args["-n"]; exists {
			if showLineNumbersBool, ok := showLineNumbers.(bool); ok && showLineNumbersBool {
				cmd = append(cmd, "-n")
			}
		}
	}

	// Add multiline flag
	if multiline, exists := args["multiline"]; exists {
		if multilineBool, ok := multiline.(bool); ok && multilineBool {
			cmd = append(cmd, "-U", "--multiline-dotall")
		}
	}

	return cmd
}

// addContextFlags adds context flags to the command (only for content mode)
func (t *GrepTool) addContextFlags(cmd []string, args map[string]interface{}, outputMode string) []string {
	if outputMode != "content" {
		return cmd
	}

	contextFlags := map[string]string{
		"-A": "-A",
		"-B": "-B",
		"-C": "-C",
	}

	for argName, flagName := range contextFlags {
		if value, exists := args[argName]; exists {
			if valueFloat, ok := value.(float64); ok {
				cmd = append(cmd, flagName, strconv.Itoa(int(valueFloat)))
			}
		}
	}

	return cmd
}

// addFilterFlags adds filter flags to the command
func (t *GrepTool) addFilterFlags(cmd []string, args map[string]interface{}) []string {
	// Add glob pattern
	if glob, exists := args["glob"]; exists {
		if globStr, ok := glob.(string); ok && globStr != "" {
			cmd = append(cmd, "--glob", globStr)
		}
	}

	// Add type filter
	if fileType, exists := args["type"]; exists {
		if fileTypeStr, ok := fileType.(string); ok && fileTypeStr != "" {
			cmd = append(cmd, "--type", fileTypeStr)
		}
	}

	return cmd
}

// getSearchPath extracts and validates the search path
func (t *GrepTool) getSearchPath(args map[string]interface{}) (string, error) {
	searchPath := "."
	if path, exists := args["path"]; exists {
		if pathStr, ok := path.(string); ok && pathStr != "" {
			searchPath = pathStr
		}
	}

	// Validate path to prevent directory traversal
	if t.isPathExcluded(searchPath) {
		return "", fmt.Errorf("access to path '%s' is not allowed", searchPath)
	}

	return searchPath, nil
}

// executeRipgrep executes the ripgrep command
func (t *GrepTool) executeRipgrep(ctx context.Context, cmd []string) ([]byte, error) {
	execCmd := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	return execCmd.Output()
}

// isNoMatchesError checks if the error is due to no matches found
func (t *GrepTool) isNoMatchesError(err error) bool {
	if exitError, ok := err.(*exec.ExitError); ok {
		return exitError.ExitCode() == 1
	}
	return false
}

// createEmptyResult creates an empty result for no matches
func (t *GrepTool) createEmptyResult(pattern, outputMode string, start time.Time) *GrepResult {
	return &GrepResult{
		Pattern:    pattern,
		OutputMode: outputMode,
		Files:      []string{},
		Matches:    []GrepMatch{},
		Counts:     []GrepCount{},
		Total:      0,
		Truncated:  false,
		Duration:   time.Since(start).String(),
	}
}

// parseRipgrepOutput parses JSON output from ripgrep
func (t *GrepTool) parseRipgrepOutput(output, outputMode string, args map[string]interface{}) (*GrepResult, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	result := &GrepResult{
		Files:   []string{},
		Matches: []GrepMatch{},
		Counts:  []GrepCount{},
	}

	// Apply head_limit if specified
	headLimit := 0
	if limit, exists := args["head_limit"]; exists {
		if limitFloat, ok := limit.(float64); ok {
			headLimit = int(limitFloat)
		}
	}

	count := 0
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Simple JSON parsing for ripgrep output
		// ripgrep JSON format varies by output mode, but we'll handle the common cases
		switch outputMode {
		case "files_with_matches":
			// Extract file path from JSON line
			if filePath := t.extractFileFromJSON(line); filePath != "" {
				if headLimit > 0 && count >= headLimit {
					result.Truncated = true
					break
				}
				result.Files = append(result.Files, filePath)
				count++
			}
		case "count":
			// Extract file and count from JSON line
			if file, matchCount := t.extractCountFromJSON(line); file != "" {
				if headLimit > 0 && count >= headLimit {
					result.Truncated = true
					break
				}
				result.Counts = append(result.Counts, GrepCount{
					File:  file,
					Count: matchCount,
				})
				count++
			}
		case "content":
			// Extract matches from JSON line
			if match := t.extractMatchFromJSON(line); match.File != "" {
				if headLimit > 0 && count >= headLimit {
					result.Truncated = true
					break
				}
				result.Matches = append(result.Matches, match)
				count++
			}
		}
	}

	result.Total = count
	return result, nil
}

// Helper functions for JSON parsing (simplified implementation)
// In a production environment, you'd use proper JSON parsing

func (t *GrepTool) extractFileFromJSON(jsonLine string) string {
	// Simple extraction for file paths in ripgrep JSON output
	// This is a simplified implementation - in production you'd use proper JSON parsing
	if strings.Contains(jsonLine, `"type":"match"`) || strings.Contains(jsonLine, `"type":"end"`) {
		// Look for "path":{"text":"filename"}
		start := strings.Index(jsonLine, `"path":{"text":"`)
		if start == -1 {
			return ""
		}
		start += len(`"path":{"text":"`)
		end := strings.Index(jsonLine[start:], `"`)
		if end == -1 {
			return ""
		}
		return jsonLine[start : start+end]
	}
	return ""
}

func (t *GrepTool) extractCountFromJSON(jsonLine string) (string, int) {
	file := t.extractFileFromJSON(jsonLine)
	if file == "" {
		return "", 0
	}

	// Look for "stats":{"matches":count}
	start := strings.Index(jsonLine, `"stats":{"matches":`)
	if start == -1 {
		return file, 0
	}
	start += len(`"stats":{"matches":`)
	end := strings.Index(jsonLine[start:], `}`)
	if end == -1 {
		return file, 0
	}

	countStr := jsonLine[start : start+end]
	if count, err := strconv.Atoi(countStr); err == nil {
		return file, count
	}
	return file, 0
}

func (t *GrepTool) extractMatchFromJSON(jsonLine string) GrepMatch {
	file := t.extractFileFromJSON(jsonLine)
	if file == "" {
		return GrepMatch{}
	}

	// Look for line number
	lineNum := 0
	if lineStart := strings.Index(jsonLine, `"line_number":`); lineStart != -1 {
		lineStart += len(`"line_number":`)
		if lineEnd := strings.Index(jsonLine[lineStart:], `,`); lineEnd != -1 {
			if num, err := strconv.Atoi(jsonLine[lineStart : lineStart+lineEnd]); err == nil {
				lineNum = num
			}
		}
	}

	// Look for line text
	text := ""
	if textStart := strings.Index(jsonLine, `"lines":{"text":"`); textStart != -1 {
		textStart += len(`"lines":{"text":"`)
		if textEnd := strings.Index(jsonLine[textStart:], `"`); textEnd != -1 {
			text = jsonLine[textStart : textStart+textEnd]
			// Unescape basic JSON escapes
			text = strings.ReplaceAll(text, `\"`, `"`)
			text = strings.ReplaceAll(text, `\\`, `\`)
		}
	}

	return GrepMatch{
		File: file,
		Line: lineNum,
		Text: text,
	}
}

// isPathExcluded checks if a file path should be excluded based on configuration
func (t *GrepTool) isPathExcluded(path string) bool {
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
