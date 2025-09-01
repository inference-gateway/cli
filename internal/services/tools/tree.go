package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	ignore "github.com/sabhiram/go-gitignore"
)

// TreeTool handles directory tree visualization operations
type TreeTool struct {
	config         *config.Config
	enabled        bool
	gitignore      *ignore.GitIgnore
	gitignoreCache map[string]*ignore.GitIgnore
	formatter      domain.BaseFormatter
}

// NewTreeTool creates a new tree tool
func NewTreeTool(cfg *config.Config) *TreeTool {
	tool := &TreeTool{
		config:         cfg,
		enabled:        cfg.Tools.Enabled && cfg.Tools.Tree.Enabled,
		formatter:      domain.NewBaseFormatter("Tree"),
		gitignoreCache: make(map[string]*ignore.GitIgnore),
	}
	tool.loadGitignore()
	return tool
}

// Definition returns the tool definition for the LLM
func (t *TreeTool) Definition() sdk.ChatCompletionTool {
	description := "Display directory structure in a tree format, similar to the Unix tree command"
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Tree",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "The path to display tree structure for (defaults to current directory)",
						"default":     ".",
					},
					"max_depth": map[string]any{
						"type":        "integer",
						"description": "Maximum depth to traverse (optional, defaults to 3 for efficiency)",
						"minimum":     1,
						"maximum":     10,
						"default":     3,
					},
					"max_files": map[string]any{
						"type":        "integer",
						"description": "Maximum number of files to display (optional, defaults to 100 for efficiency)",
						"minimum":     1,
						"maximum":     1000,
						"default":     100,
					},
					"respect_gitignore": map[string]any{
						"type":        "boolean",
						"description": "Whether to automatically exclude patterns from .gitignore (defaults to true)",
						"default":     true,
					},
					"show_hidden": map[string]any{
						"type":        "boolean",
						"description": "Whether to show hidden files and directories (defaults to false)",
						"default":     false,
					},
					"format": map[string]any{
						"type":        "string",
						"description": "Output format (text or json)",
						"enum":        []string{"text", "json"},
						"default":     "text",
					},
				},
				"required": []string{},
			},
		},
	}
}

// Execute runs the tree tool with given arguments
func (t *TreeTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("tree tool is not enabled")
	}

	path := "."
	if pathArg, ok := args["path"].(string); ok && pathArg != "" {
		path = pathArg
	}

	maxDepth := 3
	if maxDepthFloat, ok := args["max_depth"].(float64); ok {
		maxDepth = int(maxDepthFloat)
	}

	maxFiles := 100
	if maxFilesFloat, ok := args["max_files"].(float64); ok {
		maxFiles = int(maxFilesFloat)
	}

	showHidden := false
	if showHiddenArg, ok := args["show_hidden"].(bool); ok {
		showHidden = showHiddenArg
	}

	respectGitignore := true
	if respectGitignoreArg, ok := args["respect_gitignore"].(bool); ok {
		respectGitignore = respectGitignoreArg
	}

	format := "text"
	if formatArg, ok := args["format"].(string); ok {
		format = formatArg
	}

	treeResult, err := t.executeTree(path, maxDepth, maxFiles, showHidden, respectGitignore, format)
	if err != nil {
		return nil, err
	}

	var toolData *domain.TreeToolResult
	if treeResult != nil {
		toolData = &domain.TreeToolResult{
			Path:            treeResult.Path,
			Output:          treeResult.Output,
			TotalFiles:      treeResult.TotalFiles,
			TotalDirs:       treeResult.TotalDirs,
			MaxDepth:        treeResult.MaxDepth,
			MaxFiles:        treeResult.MaxFiles,
			ShowHidden:      treeResult.ShowHidden,
			Format:          treeResult.Format,
			UsingNativeTree: treeResult.UsingNativeTree,
			Truncated:       treeResult.Truncated,
		}
	}

	result := &domain.ToolExecutionResult{
		ToolName:  "Tree",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      toolData,
	}

	return result, nil
}

// Validate checks if the tree tool arguments are valid
func (t *TreeTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("tree tool is not enabled")
	}

	if path, ok := args["path"].(string); ok && path != "" {
		if err := t.validatePathSecurity(path); err != nil {
			return err
		}
	}

	if maxDepth, ok := args["max_depth"]; ok {
		maxDepthFloat, isFloat := maxDepth.(float64)
		if !isFloat {
			return fmt.Errorf("max_depth must be a number")
		}
		if maxDepthFloat < 1 || maxDepthFloat > 10 {
			return fmt.Errorf("max_depth must be between 1 and 10")
		}
	}

	if maxFiles, ok := args["max_files"]; ok {
		maxFilesFloat, isFloat := maxFiles.(float64)
		if !isFloat {
			return fmt.Errorf("max_files must be a number")
		}
		if maxFilesFloat < 1 || maxFilesFloat > 1000 {
			return fmt.Errorf("max_files must be between 1 and 1000")
		}
	}

	if showHidden, ok := args["show_hidden"]; ok {
		if _, ok := showHidden.(bool); !ok {
			return fmt.Errorf("show_hidden must be a boolean")
		}
	}

	if respectGitignore, ok := args["respect_gitignore"]; ok {
		if _, ok := respectGitignore.(bool); !ok {
			return fmt.Errorf("respect_gitignore must be a boolean")
		}
	}

	if format, ok := args["format"]; ok {
		formatStr, isString := format.(string)
		if !isString {
			return fmt.Errorf("format must be a string")
		}
		if formatStr != "text" && formatStr != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	}

	return nil
}

// IsEnabled returns whether the tree tool is enabled
func (t *TreeTool) IsEnabled() bool {
	return t.enabled
}

// TreeResult represents the internal result of a tree operation
type TreeResult struct {
	Path            string `json:"path"`
	Output          string `json:"output"`
	TotalFiles      int    `json:"total_files"`
	TotalDirs       int    `json:"total_dirs"`
	MaxDepth        int    `json:"max_depth"`
	MaxFiles        int    `json:"max_files"`
	ShowHidden      bool   `json:"show_hidden"`
	Format          string `json:"format"`
	UsingNativeTree bool   `json:"using_native_tree"`
	Truncated       bool   `json:"truncated"`
}

// executeTree performs the tree operation
func (t *TreeTool) executeTree(path string, maxDepth, maxFiles int, showHidden, respectGitignore bool, format string) (*TreeResult, error) {

	result := &TreeResult{
		Path:       path,
		MaxDepth:   maxDepth,
		MaxFiles:   maxFiles,
		ShowHidden: showHidden,
		Format:     format,
	}

	if err := t.validatePath(path); err != nil {
		return nil, err
	}

	output, files, dirs, truncated, err := t.buildTreeFallback(path, maxDepth, maxFiles, showHidden, respectGitignore, format)
	if err != nil {
		return nil, err
	}

	result.Output = output
	result.TotalFiles = files
	result.TotalDirs = dirs
	result.Truncated = truncated
	result.UsingNativeTree = false

	return result, nil
}

// buildTreeFallback builds a tree structure using our own implementation
func (t *TreeTool) buildTreeFallback(rootPath string, maxDepth, maxFiles int, showHidden, respectGitignore bool, format string) (string, int, int, bool, error) {
	fileCounter := &fileCounter{max: maxFiles}

	if format == "json" {
		textOutput, files, dirs, truncated, err := t.buildTextTree(rootPath, maxDepth, showHidden, respectGitignore, "", 0, fileCounter)
		if err != nil {
			return "", 0, 0, false, err
		}
		jsonOutput := fmt.Sprintf(`{"tree": %q, "total_files": %d, "total_dirs": %d, "truncated": %t}`, textOutput, files, dirs, truncated)
		return jsonOutput, files, dirs, truncated, nil
	}

	output, files, dirs, truncated, err := t.buildTextTree(rootPath, maxDepth, showHidden, respectGitignore, "", 0, fileCounter)
	if err != nil {
		return "", 0, 0, false, err
	}

	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("%s\n", rootPath))
	builder.WriteString(output)
	if truncated {
		builder.WriteString(fmt.Sprintf("\n... (truncated at %d files for efficiency)\n", maxFiles))
	}
	builder.WriteString(fmt.Sprintf("\n%d directories, %d files", dirs, files))
	if truncated {
		builder.WriteString(" (partial)")
	}
	builder.WriteString("\n")

	return builder.String(), files, dirs, truncated, nil
}

// fileCounter tracks file count with limit
type fileCounter struct {
	count int
	max   int
}

func (fc *fileCounter) canAdd() bool {
	return fc.count < fc.max
}

func (fc *fileCounter) add() {
	fc.count++
}

func (fc *fileCounter) isTruncated() bool {
	return fc.count >= fc.max
}

// buildTextTree recursively builds a text tree representation
func (t *TreeTool) buildTextTree(dirPath string, maxDepth int, showHidden, respectGitignore bool, prefix string, currentDepth int, fc *fileCounter) (string, int, int, bool, error) {
	if maxDepth > 0 && currentDepth >= maxDepth {
		return "", 0, 0, false, nil
	}

	if fc.isTruncated() {
		return "", 0, 0, true, nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return "", 0, 0, false, fmt.Errorf("failed to read directory %s: %w", dirPath, err)
	}

	var filteredEntries []os.DirEntry
	for _, entry := range entries {
		name := entry.Name()

		if !showHidden && strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(dirPath, name)
		if t.shouldExclude(fullPath, name, respectGitignore) {
			continue
		}

		filteredEntries = append(filteredEntries, entry)
	}

	sort.Slice(filteredEntries, func(i, j int) bool {
		if filteredEntries[i].IsDir() != filteredEntries[j].IsDir() {
			return filteredEntries[i].IsDir()
		}
		return filteredEntries[i].Name() < filteredEntries[j].Name()
	})

	var builder strings.Builder
	var totalFiles, totalDirs int
	var anyTruncated bool

	for i, entry := range filteredEntries {
		if fc.isTruncated() {
			anyTruncated = true
			break
		}

		isLast := i == len(filteredEntries)-1
		var connector, newPrefix string

		if isLast {
			connector = "└── "
			newPrefix = prefix + "    "
		} else {
			connector = "├── "
			newPrefix = prefix + "│   "
		}

		builder.WriteString(fmt.Sprintf("%s%s%s\n", prefix, connector, entry.Name()))

		if entry.IsDir() {
			totalDirs++
			subFiles, subDirs, subTruncated := t.processDirectory(dirPath, entry.Name(), maxDepth, showHidden, respectGitignore, newPrefix, currentDepth, fc, &builder)
			totalFiles += subFiles
			totalDirs += subDirs
			if subTruncated {
				anyTruncated = true
			}
			continue
		}

		if !fc.canAdd() {
			anyTruncated = true
			break
		}
		totalFiles++
		fc.add()
	}

	return builder.String(), totalFiles, totalDirs, anyTruncated, nil
}

// processDirectory handles directory processing to reduce complexity
func (t *TreeTool) processDirectory(dirPath, entryName string, maxDepth int, showHidden, respectGitignore bool, newPrefix string, currentDepth int, fc *fileCounter, builder *strings.Builder) (int, int, bool) {
	subPath := filepath.Join(dirPath, entryName)
	subOutput, subFiles, subDirs, subTruncated, err := t.buildTextTree(subPath, maxDepth, showHidden, respectGitignore, newPrefix, currentDepth+1, fc)
	if err != nil {
		return 0, 0, false
	}
	builder.WriteString(subOutput)
	return subFiles, subDirs, subTruncated
}

// shouldExclude checks if a filename should be excluded based on gitignore
func (t *TreeTool) shouldExclude(fullPath string, name string, respectGitignore bool) bool {
	if respectGitignore && t.isPathExcludedByGitignore(fullPath) {
		return true
	}
	return false
}

// isPathExcludedByGitignore checks if a path is excluded by gitignore rules
func (t *TreeTool) isPathExcludedByGitignore(fullPath string) bool {
	if t.gitignore != nil && t.gitignore.MatchesPath(fullPath) {
		return true
	}

	dirPath := filepath.Dir(fullPath)
	for dirPath != "." && dirPath != "/" {
		if dirIgnore := t.getOrLoadDirGitignore(dirPath); dirIgnore != nil {
			relPath, err := filepath.Rel(dirPath, fullPath)
			if err == nil && dirIgnore.MatchesPath(relPath) {
				return true
			}
		}

		parentDir := filepath.Dir(dirPath)
		if parentDir == dirPath {
			break
		}
		dirPath = parentDir
	}
	return false
}

// validatePathSecurity checks if a path is allowed (no file existence check)
func (t *TreeTool) validatePathSecurity(path string) error {
	return t.config.ValidatePathInSandbox(path)
}

// validatePath checks if a path exists and is accessible
func (t *TreeTool) validatePath(path string) error {
	if err := t.validatePathSecurity(path); err != nil {
		return err
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("path %s does not exist", path)
		}
		return fmt.Errorf("cannot access path %s: %w", path, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", path)
	}

	return nil
}

// loadGitignore loads .gitignore patterns using the gitignore library
func (t *TreeTool) loadGitignore() {
	gitignore, err := ignore.CompileIgnoreFileAndLines(".gitignore",
		".git/",
		".DS_Store",
		".infer/",
	)
	if err != nil {
		gitignore = ignore.CompileIgnoreLines(
			".git/",
			".DS_Store",
			".infer/",
		)
	}
	t.gitignore = gitignore
}

// getOrLoadDirGitignore loads and caches .gitignore for a specific directory
func (t *TreeTool) getOrLoadDirGitignore(dirPath string) *ignore.GitIgnore {
	if cached, exists := t.gitignoreCache[dirPath]; exists {
		return cached
	}

	gitignorePath := filepath.Join(dirPath, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		gitignore, err := ignore.CompileIgnoreFile(gitignorePath)
		if err == nil {
			t.gitignoreCache[dirPath] = gitignore
			return gitignore
		}
	}

	t.gitignoreCache[dirPath] = nil
	return nil
}

// FormatResult formats tool execution results for different contexts
func (t *TreeTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *TreeTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	treeResult, ok := result.Data.(*domain.TreeToolResult)
	if !ok {
		if result.Success {
			return "Directory tree generated successfully"
		}
		return "Directory tree generation failed"
	}

	pathName := t.formatter.GetFileName(treeResult.Path)

	var parts []string
	if treeResult.TotalDirs > 0 {
		parts = append(parts, fmt.Sprintf("%d directories", treeResult.TotalDirs))
	}
	if treeResult.TotalFiles > 0 {
		parts = append(parts, fmt.Sprintf("%d files", treeResult.TotalFiles))
	}

	status := fmt.Sprintf("Tree of %s", pathName)
	if len(parts) > 0 {
		status += fmt.Sprintf(" (%s)", strings.Join(parts, ", "))
	}

	if treeResult.Truncated {
		status += " [truncated]"
	}

	return status
}

// FormatForUI formats the result for UI display
func (t *TreeTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *TreeTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatter.FormatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatTreeData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatTreeData formats tree-specific data
func (t *TreeTool) formatTreeData(data any) string {
	treeResult, ok := data.(*domain.TreeToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("Path: %s\n", treeResult.Path))
	output.WriteString(fmt.Sprintf("Total Files: %d\n", treeResult.TotalFiles))
	output.WriteString(fmt.Sprintf("Total Directories: %d\n", treeResult.TotalDirs))
	output.WriteString(fmt.Sprintf("Max Depth: %d\n", treeResult.MaxDepth))
	output.WriteString(fmt.Sprintf("Max Files: %d\n", treeResult.MaxFiles))
	output.WriteString(fmt.Sprintf("Format: %s\n", treeResult.Format))
	output.WriteString(fmt.Sprintf("Show Hidden: %t\n", treeResult.ShowHidden))
	output.WriteString(fmt.Sprintf("Using Native Tree: %t\n", treeResult.UsingNativeTree))
	output.WriteString(fmt.Sprintf("Truncated: %t\n", treeResult.Truncated))

	if treeResult.Output != "" {
		output.WriteString(fmt.Sprintf("\nTree Output:\n%s\n", treeResult.Output))
	}

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TreeTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TreeTool) ShouldAlwaysExpand() bool {
	return false
}
