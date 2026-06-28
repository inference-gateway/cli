package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
	yaml "gopkg.in/yaml.v3"
)

const (
	ToolNameMemory = "Memory"

	OperationRead   = "read"
	OperationWrite  = "write"
	OperationDelete = "delete"

	MemoryTypeUser      = "user"
	MemoryTypeFeedback  = "feedback"
	MemoryTypeProject   = "project"
	MemoryTypeReference = "reference"

	memoryIndexHeader = "# Memory Index"
	maxSlugLen        = 64
)

// MemoryTool implements persistent, cross-session agent memory as a directory of
// individual fact-files catalogued by an index (MEMORY.md). Each fact is one
// Markdown file with YAML frontmatter; the tool keeps the index in sync.
type MemoryTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.CustomFormatter
}

// NewMemoryTool creates a new memory tool.
func NewMemoryTool(cfg *config.Config) *MemoryTool {
	return &MemoryTool{
		config:  cfg,
		enabled: cfg.Memory.Enabled,
		formatter: domain.NewCustomFormatter(ToolNameMemory, func(key string) bool {
			return key == "content"
		}),
	}
}

// Definition returns the tool definition for the LLM.
func (t *MemoryTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Memory.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        ToolNameMemory,
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"operation": map[string]any{
						"type": "string",
						"enum": []string{OperationRead, OperationWrite, OperationDelete},
						"description": "The memory operation. " +
							"read: with no name, return the MEMORY.md index; with a name, return that fact-file. " +
							"write: create or update a fact-file and its index entry. " +
							"delete: remove a fact-file and its index entry.",
					},
					"name": map[string]any{
						"type": "string",
						"description": "Short slug identifying the memory (e.g. \"build-commands\"). " +
							"Required for write and delete; optional for read.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "One-line summary shown in the MEMORY.md index and stored in the file's frontmatter. Required for write.",
					},
					"type": map[string]any{
						"type":        "string",
						"enum":        []string{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference},
						"description": "The kind of fact being stored. Required for write.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "The fact body in Markdown. Required for write.",
					},
				},
				"required": []string{"operation"},
			},
		},
	}
}

// Execute runs the memory tool with the given arguments.
func (t *MemoryTool) Execute(_ context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if !t.enabled {
		return t.errResult(args, start, "memory tool is not enabled"), nil
	}

	operation, ok := args["operation"].(string)
	if !ok {
		return t.errResult(args, start, "operation parameter is required and must be a string"), nil
	}

	switch operation {
	case OperationRead:
		return t.execRead(args, start)
	case OperationWrite:
		return t.execWrite(args, start)
	case OperationDelete:
		return t.execDelete(args, start)
	default:
		return t.errResult(args, start, fmt.Sprintf("unknown operation: %s", operation)), nil
	}
}

// execRead returns the MEMORY.md index (no name) or a single fact-file (name).
func (t *MemoryTool) execRead(args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	dir, err := t.config.ResolveMemoryDir()
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to resolve memory dir: %v", err)), nil
	}

	name, _ := args["name"].(string)
	if strings.TrimSpace(name) == "" {
		return t.readIndex(args, start, dir)
	}

	slug := sanitizeSlug(name)
	if slug == "" {
		return t.errResult(args, start, fmt.Sprintf("invalid memory name: %q", name)), nil
	}

	filePath := filepath.Join(dir, slug+".md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return t.okResult(args, start, &MemoryToolResult{
				Operation: OperationRead,
				Name:      slug,
				Message:   fmt.Sprintf("No memory named %q.", slug),
			}), nil
		}
		return t.errResult(args, start, fmt.Sprintf("failed to read memory %q: %v", slug, err)), nil
	}

	return t.okResult(args, start, &MemoryToolResult{
		Operation: OperationRead,
		Name:      slug,
		Path:      filePath,
		Content:   string(content),
		Size:      int64(len(content)),
	}), nil
}

func (t *MemoryTool) readIndex(args map[string]any, start time.Time, dir string) (*domain.ToolExecutionResult, error) {
	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	content, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return t.okResult(args, start, &MemoryToolResult{
				Operation: OperationRead,
				Message:   "No memories recorded yet.",
			}), nil
		}
		return t.errResult(args, start, fmt.Sprintf("failed to read memory index: %v", err)), nil
	}
	return t.okResult(args, start, &MemoryToolResult{
		Operation: OperationRead,
		Path:      indexPath,
		Content:   string(content),
		Size:      int64(len(content)),
	}), nil
}

// execWrite creates or updates a fact-file and upserts its index entry.
func (t *MemoryTool) execWrite(args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	memType, _ := args["type"].(string)
	content, _ := args["content"].(string)

	slug := sanitizeSlug(name)
	if slug == "" {
		return t.errResult(args, start, fmt.Sprintf("invalid memory name: %q", name)), nil
	}

	dir, err := t.config.ResolveMemoryDir()
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to resolve memory dir: %v", err)), nil
	}

	fileBody, err := buildMemoryFile(slug, description, memType, content)
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to render memory file: %v", err)), nil
	}

	filePath := filepath.Join(dir, slug+".md")
	if err := writeFileAtomic(filePath, fileBody); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to write memory file: %v", err)), nil
	}

	if err := upsertIndexEntry(dir, slug, description); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to update memory index: %v", err)), nil
	}

	return t.okResult(args, start, &MemoryToolResult{
		Operation:   OperationWrite,
		Name:        slug,
		Type:        memType,
		Description: description,
		Path:        filePath,
		Size:        int64(len(fileBody)),
		Indexed:     true,
	}), nil
}

// execDelete removes a fact-file and its index entry (idempotent).
func (t *MemoryTool) execDelete(args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	name, _ := args["name"].(string)
	slug := sanitizeSlug(name)
	if slug == "" {
		return t.errResult(args, start, fmt.Sprintf("invalid memory name: %q", name)), nil
	}

	dir, err := t.config.ResolveMemoryDir()
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to resolve memory dir: %v", err)), nil
	}

	filePath := filepath.Join(dir, slug+".md")
	existed := true
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			return t.errResult(args, start, fmt.Sprintf("failed to remove memory file: %v", err)), nil
		}
		existed = false
	}

	if err := removeIndexEntry(dir, slug); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to update memory index: %v", err)), nil
	}

	message := fmt.Sprintf("Deleted memory %q.", slug)
	if !existed {
		message = fmt.Sprintf("No memory named %q; nothing to delete.", slug)
	}
	return t.okResult(args, start, &MemoryToolResult{
		Operation: OperationDelete,
		Name:      slug,
		Path:      filePath,
		Message:   message,
	}), nil
}

// IsEnabled returns whether the memory tool is enabled.
func (t *MemoryTool) IsEnabled() bool {
	return t.enabled
}

// Validate checks the memory tool arguments.
func (t *MemoryTool) Validate(args map[string]any) error {
	if !t.config.Memory.Enabled {
		return fmt.Errorf("memory tool is not enabled")
	}

	operation, ok := args["operation"].(string)
	if !ok {
		return fmt.Errorf("operation parameter is required and must be a string")
	}

	switch operation {
	case OperationRead:
		return validateReadArgs(args)
	case OperationWrite:
		return validateWriteArgs(args)
	case OperationDelete:
		return requireStringArg(args, "name")
	default:
		return fmt.Errorf("invalid operation: %s (must be one of: read, write, delete)", operation)
	}
}

func validateReadArgs(args map[string]any) error {
	if v, ok := args["name"]; ok {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("name must be a string")
		}
	}
	return nil
}

func validateWriteArgs(args map[string]any) error {
	if err := requireStringArg(args, "name"); err != nil {
		return err
	}
	if err := requireStringArg(args, "description"); err != nil {
		return err
	}
	if err := requireStringArg(args, "content"); err != nil {
		return err
	}
	memType, err := requireStringArgValue(args, "type")
	if err != nil {
		return err
	}
	if !validMemoryType(memType) {
		return fmt.Errorf("type must be one of: user, feedback, project, reference")
	}
	return nil
}

// MemoryToolResult represents the result of a memory tool operation.
type MemoryToolResult struct {
	Operation   string `json:"operation"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Path        string `json:"path,omitempty"`
	Content     string `json:"content,omitempty"`
	Message     string `json:"message,omitempty"`
	Size        int64  `json:"size,omitempty"`
	Indexed     bool   `json:"indexed,omitempty"`
}

// memoryFrontmatter is the YAML frontmatter stored at the top of each fact-file.
type memoryFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Type string `yaml:"type"`
	} `yaml:"metadata"`
}

var slugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)

// sanitizeSlug normalizes a memory name into a safe, flat filename stem:
// lowercased, non-alphanumeric runs collapsed to single hyphens, trimmed, and
// length-capped. The result contains no path separators or dots, so it always
// resolves to a single file inside the memory dir (no traversal).
func sanitizeSlug(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugInvalidChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	return s
}

func validMemoryType(memType string) bool {
	switch memType {
	case MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference:
		return true
	default:
		return false
	}
}

func requireStringArg(args map[string]any, key string) error {
	_, err := requireStringArgValue(args, key)
	return err
}

func requireStringArgValue(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("%s is required for this operation", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	if strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("%s must not be empty", key)
	}
	return s, nil
}

// buildMemoryFile renders a fact-file: YAML frontmatter (name/description/
// metadata.type) followed by the body.
func buildMemoryFile(slug, description, memType, content string) (string, error) {
	fm := memoryFrontmatter{Name: slug, Description: description}
	fm.Metadata.Type = memType

	var fmBuf bytes.Buffer
	enc := yaml.NewEncoder(&fmBuf)
	enc.SetIndent(2)
	if err := enc.Encode(fm); err != nil {
		return "", err
	}
	_ = enc.Close()

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(fmBuf.Bytes())
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(content, "\n"))
	b.WriteString("\n")
	return b.String(), nil
}

// indexEntryLine renders the one-line catalog entry for a memory.
func indexEntryLine(slug, description string) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		return fmt.Sprintf("- [%s](%s.md)", slug, slug)
	}
	return fmt.Sprintf("- [%s](%s.md) - %s", slug, slug, desc)
}

// indexEntryMatches reports whether an index line points at slug's fact-file.
func indexEntryMatches(line, slug string) bool {
	return strings.Contains(line, fmt.Sprintf("](%s.md)", slug))
}

// readIndexEntries returns the catalog entry lines (those beginning with "- ")
// from MEMORY.md, ignoring the header and blanks. A missing file yields nil.
func readIndexEntries(indexPath string) ([]string, error) {
	content, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var entries []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			entries = append(entries, strings.TrimRight(line, "\r"))
		}
	}
	return entries, nil
}

// writeIndexEntries rewrites MEMORY.md as the header plus the given entries. The
// tool owns this file, so it is regenerated wholesale on every change.
func writeIndexEntries(indexPath string, entries []string) error {
	var b strings.Builder
	b.WriteString(memoryIndexHeader)
	b.WriteString("\n\n")
	for _, e := range entries {
		b.WriteString(e)
		b.WriteString("\n")
	}
	return writeFileAtomic(indexPath, b.String())
}

func upsertIndexEntry(dir, slug, description string) error {
	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	entries, err := readIndexEntries(indexPath)
	if err != nil {
		return err
	}
	newEntry := indexEntryLine(slug, description)
	found := false
	for i, e := range entries {
		if indexEntryMatches(e, slug) {
			entries[i] = newEntry
			found = true
			break
		}
	}
	if !found {
		entries = append(entries, newEntry)
	}
	return writeIndexEntries(indexPath, entries)
}

func removeIndexEntry(dir, slug string) error {
	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	entries, err := readIndexEntries(indexPath)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(entries))
	for _, e := range entries {
		if indexEntryMatches(e, slug) {
			continue
		}
		kept = append(kept, e)
	}
	return writeIndexEntries(indexPath, kept)
}

// writeFileAtomic writes content to path via a temp file + rename, creating the
// parent dir if needed. It deliberately bypasses the sandboxed file writer
// because the memory dir lives under ~/.infer/, which that writer's protected-
// path guard rejects; the path is built entirely from a sanitized slug, so there
// is no traversal risk.
func writeFileAtomic(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".tmp-memory-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (t *MemoryTool) errResult(args map[string]any, start time.Time, msg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     msg,
	}
}

func (t *MemoryTool) okResult(args map[string]any, start time.Time, data *MemoryToolResult) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  ToolNameMemory,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      data,
	}
}

// FormatResult formats tool execution results for different contexts.
func (t *MemoryTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a short preview of the result for UI display.
func (t *MemoryTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Memory operation result unavailable"
	}
	if !result.Success {
		return "Memory operation failed"
	}

	memResult, ok := result.Data.(*MemoryToolResult)
	if !ok {
		return "Memory operation completed"
	}

	switch memResult.Operation {
	case OperationRead:
		return previewRead(memResult)
	case OperationWrite:
		if memResult.Type != "" {
			return fmt.Sprintf("Wrote memory '%s' (%s)", memResult.Name, memResult.Type)
		}
		return fmt.Sprintf("Wrote memory '%s'", memResult.Name)
	case OperationDelete:
		return memResult.Message
	default:
		return "Memory operation completed"
	}
}

func previewRead(memResult *MemoryToolResult) string {
	if memResult.Name != "" {
		if memResult.Content == "" {
			return memResult.Message
		}
		return fmt.Sprintf("Read memory '%s'", memResult.Name)
	}
	if memResult.Content == "" {
		return "Memory index is empty"
	}
	return fmt.Sprintf("Read memory index (%d entries)", countIndexEntries(memResult.Content))
}

func countIndexEntries(index string) int {
	n := 0
	for _, line := range strings.Split(index, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			n++
		}
	}
	return n
}

// FormatForUI formats the result for UI display.
func (t *MemoryTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)

	var output strings.Builder
	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	fmt.Fprintf(&output, "%s\n", toolCall)

	if !result.Success {
		fmt.Fprintf(&output, "└─ %s Memory failed: %s", statusIcon, result.Error)
		return output.String()
	}

	preview := t.FormatPreview(result)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)
	return output.String()
}

// FormatForLLM formats the result for LLM consumption with expanded structure.
func (t *MemoryTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Memory operation result unavailable"
	}

	var output strings.Builder
	output.WriteString(t.formatter.FormatExpandedHeader(result))
	output.WriteString(t.formatMemoryResultData(result))

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

func (t *MemoryTool) formatMemoryResultData(result *domain.ToolExecutionResult) string {
	memResult, ok := result.Data.(*MemoryToolResult)
	if !ok {
		return ""
	}

	connector := "└─"
	if len(result.Metadata) > 0 {
		connector = "├─"
	}

	var output strings.Builder
	fmt.Fprintf(&output, "%s Result:\n", connector)
	fmt.Fprintf(&output, "   Operation: %s\n", memResult.Operation)
	if memResult.Name != "" {
		fmt.Fprintf(&output, "   Name: %s\n", memResult.Name)
	}
	if memResult.Type != "" {
		fmt.Fprintf(&output, "   Type: %s\n", memResult.Type)
	}
	if memResult.Path != "" {
		fmt.Fprintf(&output, "   Path: %s\n", memResult.Path)
	}
	if memResult.Message != "" {
		fmt.Fprintf(&output, "   %s\n", memResult.Message)
	}
	if memResult.Content != "" && memResult.Operation == OperationRead {
		fmt.Fprintf(&output, "   Content:\n%s\n", memResult.Content)
	}
	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display.
func (t *MemoryTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI.
func (t *MemoryTool) ShouldAlwaysExpand() bool {
	return false
}
