package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	mocks "github.com/inference-gateway/cli/tests/mocks/domain"

	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	project "github.com/inference-gateway/cli/internal/project"
)

// testProjectIdentity is the zero identity (global scope): the pre-existing
// tests assert root-level fact files, which is exactly the no-project case.
func testProjectIdentity() project.Identity {
	return project.Identity{}
}

func newTestMemoryTool(t *testing.T) (*MemoryTool, string) {
	t.Helper()
	return newTestMemoryToolWithProject(t, testProjectIdentity())
}

func newTestMemoryToolWithProject(t *testing.T, proj project.Identity) (*MemoryTool, string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Dir = t.TempDir()
	cfg.Memory.MaxChars = config.DefaultMemoryMaxChars
	cfg.Prompts = *config.DefaultPromptsConfig()
	return NewMemoryTool(cfg, nil, proj), cfg.Memory.Dir
}

func TestMemoryTool_SyncOutOnMutation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = true
	cfg.Memory.Dir = t.TempDir()
	cfg.Memory.MaxChars = config.DefaultMemoryMaxChars
	cfg.Prompts = *config.DefaultPromptsConfig()

	fake := &mocks.FakeMemoryBackend{}
	tool := NewMemoryTool(cfg, fake, testProjectIdentity())

	if _, err := tool.Execute(context.Background(), map[string]any{"operation": "read"}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := fake.SyncOutCallCount(); got != 0 {
		t.Fatalf("read must not sync out; got %d calls", got)
	}

	execOK(t, tool, map[string]any{
		"operation":   "write",
		"name":        "build-commands",
		"description": "how to build",
		"type":        "project",
		"content":     "run task build",
	})
	if got := fake.SyncOutCallCount(); got != 1 {
		t.Fatalf("write must sync out once; got %d calls", got)
	}

	execOK(t, tool, map[string]any{"operation": "delete", "name": "build-commands"})
	if got := fake.SyncOutCallCount(); got != 2 {
		t.Fatalf("delete must sync out; got %d calls", got)
	}
}

func execOK(t *testing.T, tool *MemoryTool, args map[string]any) *MemoryToolResult {
	t.Helper()
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Fatalf("Expected success, got error: %s", res.Error)
	}
	data, ok := res.Data.(*MemoryToolResult)
	if !ok {
		t.Fatalf("Expected *MemoryToolResult, got %T", res.Data)
	}
	return data
}

func readIndexFile(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, config.MemoryIndexFileName))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	return string(data)
}

func parseFrontmatter(t *testing.T, content string) memoryFrontmatter {
	t.Helper()
	rest := strings.TrimPrefix(content, "---\n")
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		t.Fatalf("no frontmatter terminator in:\n%s", content)
	}
	var fm memoryFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:idx]), &fm); err != nil {
		t.Fatalf("unmarshal frontmatter: %v", err)
	}
	return fm
}

func TestMemoryTool_Definition(t *testing.T) {
	tool, _ := newTestMemoryTool(t)

	def := tool.Definition()
	if def.Function.Name != "Memory" {
		t.Errorf("Expected tool name 'Memory', got '%s'", def.Function.Name)
	}
	if def.Function.Description == nil || *def.Function.Description == "" {
		t.Error("Tool description should not be empty")
	}
	if def.Function.Parameters == nil {
		t.Fatal("Tool parameters should not be nil")
	}

	props, ok := (*def.Function.Parameters)["properties"].(map[string]any)
	if !ok {
		t.Fatal("Expected properties map in parameters")
	}
	for _, key := range []string{"operation", "name", "description", "type", "content"} {
		if _, ok := props[key]; !ok {
			t.Errorf("Expected parameter %q to be declared", key)
		}
	}

	op, _ := props["operation"].(map[string]any)
	enum, _ := op["enum"].([]string)
	if got := strings.Join(enum, ","); got != "read,write,delete" {
		t.Errorf("Expected operation enum read,write,delete, got %q", got)
	}
}

func TestMemoryTool_IsEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	if NewMemoryTool(cfg, nil, testProjectIdentity()).IsEnabled() {
		t.Error("Memory tool should be disabled by default")
	}

	cfg.Memory.Enabled = true
	if !NewMemoryTool(cfg, nil, testProjectIdentity()).IsEnabled() {
		t.Error("Memory tool should be enabled when memory.enabled is true")
	}
}

func TestMemoryTool_Validate(t *testing.T) {
	tool, _ := newTestMemoryTool(t)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errMsg  string
	}{
		{name: "read no name", args: map[string]any{"operation": "read"}},
		{name: "read with name", args: map[string]any{"operation": "read", "name": "foo"}},
		{name: "read non-string name", args: map[string]any{"operation": "read", "name": 7}, wantErr: true, errMsg: "name"},
		{
			name: "write valid",
			args: map[string]any{"operation": "write", "name": "foo", "description": "a fact", "type": "project", "content": "body"},
		},
		{name: "write missing name", args: map[string]any{"operation": "write", "description": "d", "type": "project", "content": "b"}, wantErr: true, errMsg: "name"},
		{name: "write missing description", args: map[string]any{"operation": "write", "name": "foo", "type": "project", "content": "b"}, wantErr: true, errMsg: "description"},
		{name: "write missing content", args: map[string]any{"operation": "write", "name": "foo", "description": "d", "type": "project"}, wantErr: true, errMsg: "content"},
		{name: "write missing type", args: map[string]any{"operation": "write", "name": "foo", "description": "d", "content": "b"}, wantErr: true, errMsg: "type"},
		{name: "write bad type", args: map[string]any{"operation": "write", "name": "foo", "description": "d", "type": "bogus", "content": "b"}, wantErr: true, errMsg: "type"},
		{name: "delete valid", args: map[string]any{"operation": "delete", "name": "foo"}},
		{name: "delete missing name", args: map[string]any{"operation": "delete"}, wantErr: true, errMsg: "name"},
		{name: "missing operation", args: map[string]any{"name": "foo"}, wantErr: true, errMsg: "operation"},
		{name: "invalid operation", args: map[string]any{"operation": "frobnicate"}, wantErr: true, errMsg: "operation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("Expected error but got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestMemoryTool_Validate_Disabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Enabled = false
	tool := NewMemoryTool(cfg, nil, testProjectIdentity())
	if err := tool.Validate(map[string]any{"operation": "read"}); err == nil {
		t.Error("Expected validation error when memory is disabled")
	}
}

func TestMemoryTool_Write_CreatesFileAndIndex(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	res := execOK(t, tool, map[string]any{
		"operation":   "write",
		"name":        "build-commands",
		"description": "task build runs the build",
		"type":        "project",
		"content":     "Run `task build` to build the binary.",
	})
	if res.Name != "build-commands" || !res.Indexed {
		t.Fatalf("unexpected write result: %+v", res)
	}

	content, err := os.ReadFile(filepath.Join(dir, "build-commands.md"))
	if err != nil {
		t.Fatalf("fact-file not created: %v", err)
	}
	fm := parseFrontmatter(t, string(content))
	if fm.Name != "build-commands" || fm.Description != "task build runs the build" || fm.Metadata.Type != "project" {
		t.Errorf("unexpected frontmatter: %+v", fm)
	}
	if !strings.Contains(string(content), "Run `task build`") {
		t.Errorf("fact body missing from file:\n%s", content)
	}

	index := readIndexFile(t, dir)
	if !strings.Contains(index, "](build-commands.md)") {
		t.Errorf("index missing entry:\n%s", index)
	}
	if n := strings.Count(index, "](build-commands.md)"); n != 1 {
		t.Errorf("expected exactly 1 index entry, got %d:\n%s", n, index)
	}
}

func TestMemoryTool_Write_UpsertNoDuplicate(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	base := map[string]any{"operation": "write", "name": "prefs", "type": "user", "content": "v1"}
	base["description"] = "likes tabs"
	execOK(t, tool, base)

	base["description"] = "likes spaces now"
	base["content"] = "v2"
	execOK(t, tool, base)

	index := readIndexFile(t, dir)
	if n := strings.Count(index, "](prefs.md)"); n != 1 {
		t.Errorf("expected exactly 1 index entry after upsert, got %d:\n%s", n, index)
	}
	if !strings.Contains(index, "likes spaces now") || strings.Contains(index, "likes tabs") {
		t.Errorf("index description not updated in place:\n%s", index)
	}

	content, _ := os.ReadFile(filepath.Join(dir, "prefs.md"))
	if !strings.Contains(string(content), "v2") || strings.Contains(string(content), "v1") {
		t.Errorf("fact-file not overwritten:\n%s", content)
	}
}

func TestMemoryTool_Write_SanitizesSlug(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	res := execOK(t, tool, map[string]any{
		"operation":   "write",
		"name":        "My Fact! v2",
		"description": "d",
		"type":        "reference",
		"content":     "b",
	})
	if res.Name != "my-fact-v2" {
		t.Errorf("expected slug 'my-fact-v2', got %q", res.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "my-fact-v2.md")); err != nil {
		t.Errorf("expected flat fact-file my-fact-v2.md: %v", err)
	}

	execRes, err := tool.Execute(context.Background(), map[string]any{
		"operation":   "write",
		"name":        "../evil",
		"description": "d",
		"type":        "reference",
		"content":     "b",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if execRes.Success {
		t.Error("a traversal name must be rejected")
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.md")); err == nil {
		t.Error("a file escaped the memory dir")
	}
}

func TestMemoryTool_Read(t *testing.T) {
	tool, _ := newTestMemoryTool(t)

	res := execOK(t, tool, map[string]any{"operation": "read"})
	if res.Content != "" {
		t.Errorf("expected empty index content, got %q", res.Content)
	}

	execOK(t, tool, map[string]any{
		"operation": "write", "name": "foo", "description": "the foo fact", "type": "project", "content": "foo body",
	})

	res = execOK(t, tool, map[string]any{"operation": "read"})
	if !strings.Contains(res.Content, "](foo.md)") {
		t.Errorf("index read missing entry:\n%s", res.Content)
	}

	res = execOK(t, tool, map[string]any{"operation": "read", "name": "foo"})
	if !strings.Contains(res.Content, "foo body") {
		t.Errorf("named read missing body:\n%s", res.Content)
	}

	res = execOK(t, tool, map[string]any{"operation": "read", "name": "missing"})
	if res.Content != "" || !strings.Contains(res.Message, "No memory named") {
		t.Errorf("expected friendly miss, got content=%q message=%q", res.Content, res.Message)
	}
}

func TestMemoryTool_Delete(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	execOK(t, tool, map[string]any{"operation": "write", "name": "foo", "description": "d", "type": "project", "content": "b"})

	execOK(t, tool, map[string]any{"operation": "delete", "name": "foo"})
	if _, err := os.Stat(filepath.Join(dir, "foo.md")); !os.IsNotExist(err) {
		t.Error("fact-file should be deleted")
	}
	if strings.Contains(readIndexFile(t, dir), "](foo.md)") {
		t.Error("index entry should be removed")
	}

	res := execOK(t, tool, map[string]any{"operation": "delete", "name": "foo"})
	if !strings.Contains(res.Message, "nothing to delete") {
		t.Errorf("expected idempotent delete message, got %q", res.Message)
	}
}

func TestMemoryTool_IndexIntegrity(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	execOK(t, tool, map[string]any{"operation": "write", "name": "alpha", "description": "a", "type": "project", "content": "x"})
	execOK(t, tool, map[string]any{"operation": "write", "name": "beta", "description": "b", "type": "project", "content": "y"})
	execOK(t, tool, map[string]any{"operation": "delete", "name": "alpha"})

	index := readIndexFile(t, dir)
	if strings.Contains(index, "](alpha.md)") {
		t.Errorf("deleted entry still in index:\n%s", index)
	}
	if !strings.Contains(index, "](beta.md)") {
		t.Errorf("surviving entry missing from index:\n%s", index)
	}
	if n := countIndexEntries(index); n != 1 {
		t.Errorf("expected 1 index entry, got %d:\n%s", n, index)
	}
}

// TestMemoryTool_ConcurrentWritesKeepAllIndexEntries guards the MEMORY.md index
// against a last-writer-wins loss: Memory tools run in the agent's parallel
// execution pool, so distinct writes in one turn race the index read-modify-
// write. Every entry must survive. Asserts on file contents, not the -race
// detector, because the loss is at the file layer, not shared memory.
func TestMemoryTool_ConcurrentWritesKeepAllIndexEntries(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	for i := range n {
		go func(i int) {
			defer wg.Done()
			if _, err := tool.Execute(context.Background(), map[string]any{
				"operation":   "write",
				"name":        fmt.Sprintf("fact-%02d", i),
				"description": fmt.Sprintf("fact number %d", i),
				"type":        "project",
				"content":     "body",
			}); err != nil {
				t.Errorf("write %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	index := readIndexFile(t, dir)
	if got := countIndexEntries(index); got != n {
		t.Fatalf("expected %d index entries after concurrent writes, got %d:\n%s", n, got, index)
	}
	for i := range n {
		if want := fmt.Sprintf("](fact-%02d.md)", i); !strings.Contains(index, want) {
			t.Errorf("index missing entry %s:\n%s", want, index)
		}
	}
}

func TestMemoryTool_NoTempLeftovers(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	execOK(t, tool, map[string]any{"operation": "write", "name": "foo", "description": "d", "type": "project", "content": "b"})
	execOK(t, tool, map[string]any{"operation": "write", "name": "foo", "description": "d2", "type": "project", "content": "b2"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-memory") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestMemoryTool_Write_ProjectScoping(t *testing.T) {
	detected := project.Identity{Name: "inference-gateway/cli", Slug: "inference-gateway-cli"}
	tool, dir := newTestMemoryToolWithProject(t, detected)

	res := execOK(t, tool, map[string]any{
		"operation": "write", "name": "build-commands", "description": "d", "type": "project", "content": "b",
	})
	if res.Name != "inference-gateway-cli/build-commands" {
		t.Errorf("expected project-prefixed name, got %q", res.Name)
	}
	path := filepath.Join(dir, "inference-gateway-cli", "build-commands.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected project fact-file: %v", err)
	}
	fm := parseFrontmatter(t, string(data))
	if fm.Metadata.Project != "inference-gateway/cli" {
		t.Errorf("frontmatter project = %q, want inference-gateway/cli", fm.Metadata.Project)
	}
	if !strings.Contains(readIndexFile(t, dir), "](inference-gateway-cli/build-commands.md)") {
		t.Errorf("index missing project entry:\n%s", readIndexFile(t, dir))
	}
}

func TestMemoryTool_Write_UserTypeDefaultsGlobal(t *testing.T) {
	detected := project.Identity{Name: "inference-gateway/cli", Slug: "inference-gateway-cli"}
	tool, dir := newTestMemoryToolWithProject(t, detected)

	res := execOK(t, tool, map[string]any{
		"operation": "write", "name": "prefers-tabs", "description": "d", "type": "user", "content": "b",
	})
	if res.Name != "prefers-tabs" {
		t.Errorf("user fact must be global, got %q", res.Name)
	}
	data, err := os.ReadFile(filepath.Join(dir, "prefers-tabs.md"))
	if err != nil {
		t.Fatalf("expected root fact-file: %v", err)
	}
	if fm := parseFrontmatter(t, string(data)); fm.Metadata.Project != "" {
		t.Errorf("global fact must have no frontmatter project, got %q", fm.Metadata.Project)
	}
}

func TestMemoryTool_Write_ExplicitProject(t *testing.T) {
	detected := project.Identity{Name: "inference-gateway/cli", Slug: "inference-gateway-cli"}
	tool, dir := newTestMemoryToolWithProject(t, detected)

	res := execOK(t, tool, map[string]any{
		"operation": "write", "name": "x", "description": "d", "type": "project", "content": "b", "project": "GLOBAL",
	})
	if res.Name != "x" {
		t.Errorf("project=global must force root, got %q", res.Name)
	}

	res = execOK(t, tool, map[string]any{
		"operation": "write", "name": "y", "description": "d", "type": "reference", "content": "b", "project": "other-org/other",
	})
	if res.Name != "other-org-other/y" {
		t.Errorf("explicit project must be slugified, got %q", res.Name)
	}
	data, err := os.ReadFile(filepath.Join(dir, "other-org-other", "y.md"))
	if err != nil {
		t.Fatalf("expected other-project fact-file: %v", err)
	}
	if fm := parseFrontmatter(t, string(data)); fm.Metadata.Project != "other-org/other" {
		t.Errorf("frontmatter project = %q, want other-org/other", fm.Metadata.Project)
	}
}

func TestMemoryTool_Write_NameEmbeddedProject(t *testing.T) {
	tool, dir := newTestMemoryToolWithProject(t, project.Identity{})

	res := execOK(t, tool, map[string]any{
		"operation": "write", "name": "p/s", "description": "d", "type": "project", "content": "b",
	})
	if res.Name != "p/s" {
		t.Errorf("expected p/s, got %q", res.Name)
	}
	if _, err := os.Stat(filepath.Join(dir, "p", "s.md")); err != nil {
		t.Fatalf("expected fact under p/: %v", err)
	}

	execRes, err := tool.Execute(context.Background(), map[string]any{
		"operation": "write", "name": "p/s", "description": "d", "type": "project", "content": "b", "project": "q",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if execRes.Success || !strings.Contains(execRes.Error, "conflicting project") {
		t.Errorf("conflicting name/project must error, got success=%v err=%q", execRes.Success, execRes.Error)
	}
}

func TestMemoryTool_ReadDelete_ProjectAndLegacy(t *testing.T) {
	tool, dir := newTestMemoryToolWithProject(t, project.Identity{Name: "o/r", Slug: "o-r"})

	execOK(t, tool, map[string]any{
		"operation": "write", "name": "x", "description": "global x", "type": "user", "content": "gx",
	})
	execOK(t, tool, map[string]any{
		"operation": "write", "name": "x", "description": "project x", "type": "project", "content": "px",
	})

	res := execOK(t, tool, map[string]any{"operation": "read", "name": "x"})
	if !strings.Contains(res.Content, "gx") {
		t.Errorf("bare read must hit the global fact:\n%s", res.Content)
	}
	res = execOK(t, tool, map[string]any{"operation": "read", "name": "o-r/x"})
	if !strings.Contains(res.Content, "px") {
		t.Errorf("prefixed read must hit the project fact:\n%s", res.Content)
	}

	res = execOK(t, tool, map[string]any{"operation": "read", "name": "o-r/missing"})
	if !strings.Contains(res.Message, "o-r/missing") {
		t.Errorf("miss message must include the full name, got %q", res.Message)
	}

	execOK(t, tool, map[string]any{"operation": "delete", "name": "o-r/x"})
	if _, err := os.Stat(filepath.Join(dir, "o-r", "x.md")); err == nil {
		t.Error("project fact must be deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "x.md")); err != nil {
		t.Error("global fact must survive the project delete")
	}
	index := readIndexFile(t, dir)
	if strings.Contains(index, "](o-r/x.md)") || !strings.Contains(index, "](x.md)") {
		t.Errorf("index must drop only the project entry:\n%s", index)
	}
}

func TestMemoryTool_Write_EntryCap(t *testing.T) {
	tool, _ := newTestMemoryTool(t)
	maxChars := tool.config.Memory.EffectiveMaxEntryChars()

	execOK(t, tool, map[string]any{
		"operation": "write", "name": "at-cap", "description": "d", "type": "reference",
		"content": strings.Repeat("a", maxChars),
	})

	res, err := tool.Execute(context.Background(), map[string]any{
		"operation": "write", "name": "over-cap", "description": "d", "type": "reference",
		"content": strings.Repeat("a", maxChars+1),
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success || !strings.Contains(res.Error, "max_entry_chars") {
		t.Errorf("over-cap write must be rejected with an actionable error, got success=%v err=%q", res.Success, res.Error)
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		in          string
		wantProject string
		wantSlug    string
		wantOK      bool
	}{
		{"build-commands", "", "build-commands", true},
		{"A B/C d", "a-b", "c-d", true},
		{"p/s", "p", "s", true},
		{"../evil", "", "", false},
		{"a/b/c", "", "", false},
		{"", "", "", false},
		{"/s", "", "", false},
		{"p/", "", "", false},
	}
	for _, tt := range tests {
		p, s, ok := sanitizeName(tt.in)
		if p != tt.wantProject || s != tt.wantSlug || ok != tt.wantOK {
			t.Errorf("sanitizeName(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.in, p, s, ok, tt.wantProject, tt.wantSlug, tt.wantOK)
		}
	}
}

func TestMemoryTool_Write_RecordsSessionID(t *testing.T) {
	tool, dir := newTestMemoryTool(t)

	ctx := domain.WithSessionID(context.Background(), "channel-telegram-12345")
	res, err := tool.Execute(ctx, map[string]any{
		"operation": "write", "name": "with-session", "description": "d", "type": "reference", "content": "b",
	})
	if err != nil || !res.Success {
		t.Fatalf("write failed: err=%v res=%+v", err, res)
	}
	data, err := os.ReadFile(filepath.Join(dir, "with-session.md"))
	if err != nil {
		t.Fatalf("read fact: %v", err)
	}
	if fm := parseFrontmatter(t, string(data)); fm.Metadata.Session != "channel-telegram-12345" {
		t.Errorf("frontmatter session = %q, want channel-telegram-12345", fm.Metadata.Session)
	}

	execOK(t, tool, map[string]any{
		"operation": "write", "name": "no-session", "description": "d", "type": "reference", "content": "b",
	})
	data, err = os.ReadFile(filepath.Join(dir, "no-session.md"))
	if err != nil {
		t.Fatalf("read fact: %v", err)
	}
	if fm := parseFrontmatter(t, string(data)); fm.Metadata.Session != "" {
		t.Errorf("session must be omitted without a session context, got %q", fm.Metadata.Session)
	}
}
