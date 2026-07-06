package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"
	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	project "github.com/inference-gateway/cli/internal/project"
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
)

// MemoryTool implements persistent, cross-session agent memory as a directory of
// individual fact-files catalogued by an index (MEMORY.md). Each fact is one
// Markdown file with YAML frontmatter; the tool keeps the index in sync.
type MemoryTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.CustomFormatter
	backend   domain.MemoryBackend
	project   project.Identity
}

// NewMemoryTool creates a new memory tool. backend syncs the memory directory to
// a remote after a write/delete (nil-safe: a nil backend, or the local no-op
// backend, means no remote sync). This is the chat push path - post_session
// would push after every message, so the tool triggers the push instead.
// proj is the detected project the process runs in (zero value = global scope);
// it decides where project-scoped facts are filed.
func NewMemoryTool(cfg *config.Config, backend domain.MemoryBackend, proj project.Identity) *MemoryTool {
	return &MemoryTool{
		config:  cfg,
		enabled: cfg.Memory.Enabled,
		project: proj,
		formatter: domain.NewCustomFormatter(ToolNameMemory, func(key string) bool {
			return key == "content"
		}),
		backend: backend,
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
						"description": "Name identifying the memory: \"<slug>\" for a global fact (e.g. \"build-commands\") or " +
							"\"<project>/<slug>\" for a project fact (e.g. \"inference-gateway-cli/build-commands\"), exactly as shown in the index. " +
							"Required for write and delete; optional for read.",
					},
					"project": map[string]any{
						"type": "string",
						"description": "Optional, write only. Where the fact belongs: omit to use the default " +
							"(user facts are global; feedback/project/reference facts go under the current project), " +
							"pass \"global\" to force a global fact, or an org/repo name to file it under another project.",
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
func (t *MemoryTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
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
		return t.execWrite(ctx, args, start)
	case OperationDelete:
		return t.execDelete(ctx, args, start)
	default:
		return t.errResult(args, start, fmt.Sprintf("unknown operation: %s", operation)), nil
	}
}

// syncOut pushes the memory directory to the remote after a change (best-effort;
// the backend logs its own failures and the local backend is a no-op).
func (t *MemoryTool) syncOut(ctx context.Context) {
	if t.backend != nil {
		_ = t.backend.SyncOut(ctx)
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

	projectSlug, slug, ok := sanitizeName(name)
	if !ok {
		return t.errResult(args, start, fmt.Sprintf("invalid memory name: %q", name)), nil
	}
	relKey := joinKey(projectSlug, slug)

	filePath := filepath.Join(dir, projectSlug, slug+".md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return t.okResult(args, start, &MemoryToolResult{
				Operation: OperationRead,
				Name:      relKey,
				Message:   fmt.Sprintf("No memory named %q (project facts are read as \"<project>/<name>\" - see the index).", relKey),
			}), nil
		}
		return t.errResult(args, start, fmt.Sprintf("failed to read memory %q: %v", relKey, err)), nil
	}

	return t.okResult(args, start, &MemoryToolResult{
		Operation: OperationRead,
		Name:      relKey,
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
func (t *MemoryTool) execWrite(ctx context.Context, args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	name, _ := args["name"].(string)
	description, _ := args["description"].(string)
	memType, _ := args["type"].(string)
	content, _ := args["content"].(string)
	projectArg, _ := args["project"].(string)

	projectSlug, slug, err := resolveWriteTarget(name, projectArg, memType, t.project)
	if err != nil {
		return t.errResult(args, start, err.Error()), nil
	}
	relKey := joinKey(projectSlug, slug)

	if maxChars := t.config.Memory.EffectiveMaxEntryChars(); len(content) > maxChars {
		return t.errResult(args, start, fmt.Sprintf(
			"memory content is %d chars, over the per-entry cap of %d; store a tighter summary or split into multiple facts (memory.max_entry_chars raises the cap)",
			len(content), maxChars)), nil
	}

	dir, err := t.config.ResolveMemoryDir()
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to resolve memory dir: %v", err)), nil
	}

	fileBody, err := buildMemoryFile(slug, description, memType, t.projectDisplayName(projectSlug, projectArg), domain.GetSessionID(ctx), content)
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to render memory file: %v", err)), nil
	}

	filePath := filepath.Join(dir, projectSlug, slug+".md")
	if err := writeFileAtomic(filePath, fileBody); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to write memory file: %v", err)), nil
	}

	if err := upsertIndexEntry(dir, relKey, description); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to update memory index: %v", err)), nil
	}

	t.syncOut(ctx)

	return t.okResult(args, start, &MemoryToolResult{
		Operation:   OperationWrite,
		Name:        relKey,
		Type:        memType,
		Project:     projectSlug,
		Description: description,
		Path:        filePath,
		Size:        int64(len(fileBody)),
		Indexed:     true,
	}), nil
}

// projectDisplayName returns the human-readable project name recorded in the
// fact's frontmatter: the detected project's name when the fact is filed under
// it, otherwise the explicit project argument as given; empty for global.
func (t *MemoryTool) projectDisplayName(projectSlug, projectArg string) string {
	if projectSlug == "" {
		return ""
	}
	if projectSlug == t.project.Slug && t.project.Name != "" {
		return t.project.Name
	}
	if arg := strings.TrimSpace(projectArg); arg != "" && !strings.EqualFold(arg, projectGlobal) {
		return arg
	}
	return projectSlug
}

// projectGlobal is the sentinel project argument that forces a global fact.
const projectGlobal = "global"

// resolveWriteTarget decides where a write lands: a project subdirectory or the
// global root. Precedence: a project embedded in the name ("p/s"), then the
// explicit project argument ("global" forces the root), then a type-based
// default - user facts are global, everything else goes under the detected
// project (global when no project was detected).
func resolveWriteTarget(name, projectArg, memType string, detected project.Identity) (projectSlug, slug string, err error) {
	nameProject, slug, ok := sanitizeName(name)
	if !ok {
		return "", "", fmt.Errorf("invalid memory name: %q", name)
	}

	argSlug := ""
	if arg := strings.TrimSpace(projectArg); arg != "" {
		if !strings.EqualFold(arg, projectGlobal) {
			argSlug = project.Slugify(arg)
			if argSlug == "" {
				return "", "", fmt.Errorf("invalid project: %q", projectArg)
			}
		}
	}

	if nameProject != "" {
		if strings.TrimSpace(projectArg) != "" && argSlug != nameProject {
			return "", "", fmt.Errorf("conflicting project: name says %q, project says %q", nameProject, projectArg)
		}
		return nameProject, slug, nil
	}
	if strings.TrimSpace(projectArg) != "" {
		return argSlug, slug, nil
	}
	if memType == MemoryTypeUser {
		return "", slug, nil
	}
	return detected.Slug, slug, nil
}

// execDelete removes a fact-file and its index entry (idempotent).
func (t *MemoryTool) execDelete(ctx context.Context, args map[string]any, start time.Time) (*domain.ToolExecutionResult, error) {
	name, _ := args["name"].(string)
	projectSlug, slug, ok := sanitizeName(name)
	if !ok {
		return t.errResult(args, start, fmt.Sprintf("invalid memory name: %q", name)), nil
	}
	relKey := joinKey(projectSlug, slug)

	dir, err := t.config.ResolveMemoryDir()
	if err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to resolve memory dir: %v", err)), nil
	}

	filePath := filepath.Join(dir, projectSlug, slug+".md")
	existed := true
	if err := os.Remove(filePath); err != nil {
		if !os.IsNotExist(err) {
			return t.errResult(args, start, fmt.Sprintf("failed to remove memory file: %v", err)), nil
		}
		existed = false
	}

	if err := removeIndexEntry(dir, relKey); err != nil {
		return t.errResult(args, start, fmt.Sprintf("failed to update memory index: %v", err)), nil
	}

	t.syncOut(ctx)

	message := fmt.Sprintf("Deleted memory %q.", relKey)
	if !existed {
		message = fmt.Sprintf("No memory named %q; nothing to delete.", relKey)
	}
	return t.okResult(args, start, &MemoryToolResult{
		Operation: OperationDelete,
		Name:      relKey,
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
	if v, ok := args["project"]; ok {
		if _, ok := v.(string); !ok {
			return fmt.Errorf("project must be a string")
		}
	}
	return nil
}

// MemoryToolResult represents the result of a memory tool operation.
type MemoryToolResult struct {
	Operation   string `json:"operation"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	Project     string `json:"project,omitempty"`
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
		Type    string `yaml:"type"`
		Project string `yaml:"project,omitempty"` // human-readable, e.g. "inference-gateway/cli"; absent for global
		Session string `yaml:"session,omitempty"` // session that last wrote the fact (chat conversation ID or headless session ID)
	} `yaml:"metadata"`
}

// sanitizeName parses a memory name into (projectSlug, slug): "p/s" ->
// ("p", "s"), "s" -> ("", "s"). Each segment is independently slugified
// (lowercased, non-alphanumeric runs collapsed to single hyphens, trimmed,
// length-capped), so no dots or separators survive and the joined path always
// stays inside the memory dir (no traversal). More than one "/" or any empty
// sanitized segment yields ok=false.
func sanitizeName(name string) (projectSlug, slug string, ok bool) {
	parts := strings.Split(strings.TrimSpace(name), "/")
	switch len(parts) {
	case 1:
		slug = project.Slugify(parts[0])
		return "", slug, slug != ""
	case 2:
		projectSlug = project.Slugify(parts[0])
		slug = project.Slugify(parts[1])
		if projectSlug == "" || slug == "" {
			return "", "", false
		}
		return projectSlug, slug, true
	default:
		return "", "", false
	}
}

// joinKey renders the index key for a fact: "p/s" or bare "s". Always a
// forward slash - the key doubles as a Markdown link target.
func joinKey(projectSlug, slug string) string {
	if projectSlug == "" {
		return slug
	}
	return projectSlug + "/" + slug
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
// metadata.type/metadata.project/metadata.session) followed by the body.
// projectName is the human-readable project (empty for a global fact);
// sessionID tracks provenance - the session that last wrote the fact.
func buildMemoryFile(slug, description, memType, projectName, sessionID, content string) (string, error) {
	fm := memoryFrontmatter{Name: slug, Description: description}
	fm.Metadata.Type = memType
	fm.Metadata.Project = projectName
	fm.Metadata.Session = sessionID

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

// indexEntryLine renders the one-line catalog entry for a memory. relKey is
// "slug" for a global fact or "project/slug" for a project fact.
func indexEntryLine(relKey, description string) string {
	desc := strings.TrimSpace(description)
	if desc == "" {
		return fmt.Sprintf("- [%s](%s.md)", relKey, relKey)
	}
	return fmt.Sprintf("- [%s](%s.md) - %s", relKey, relKey, desc)
}

// indexEntryMatches reports whether an index line points at relKey's fact-file.
// The "](<relKey>.md)" needle is unambiguous across the global and project
// forms: "](s.md)" never occurs inside "](p/s.md)".
func indexEntryMatches(line, relKey string) bool {
	return strings.Contains(line, fmt.Sprintf("](%s.md)", relKey))
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

// indexMu serializes the read-modify-write of the shared MEMORY.md index.
// Memory tools run in the agent's parallel execution pool, so two writes in one
// turn can otherwise both read the same index and the second's atomic rewrite
// silently drops the first's entry (a last-writer-wins loss the -race detector
// can't see because it is a file, not shared memory).
var indexMu sync.Mutex

func upsertIndexEntry(dir, relKey, description string) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	entries, err := readIndexEntries(indexPath)
	if err != nil {
		return err
	}
	newEntry := indexEntryLine(relKey, description)
	found := false
	for i, e := range entries {
		if indexEntryMatches(e, relKey) {
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

func removeIndexEntry(dir, relKey string) error {
	indexMu.Lock()
	defer indexMu.Unlock()
	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	entries, err := readIndexEntries(indexPath)
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(entries))
	for _, e := range entries {
		if indexEntryMatches(e, relKey) {
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
