package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/inference-gateway/cli/config"
)

func newPlanToolForTest(t *testing.T) (*RequestPlanApprovalTool, string) {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Prompts: *config.DefaultPromptsConfig(),
	}
	cfg.SetConfigDir(tmpDir)
	tool := NewRequestPlanApprovalTool(cfg)
	tool.now = func() time.Time {
		return time.Date(2026, 4, 27, 10, 30, 15, 0, time.UTC)
	}
	return tool, tmpDir
}

func TestRequestPlanApprovalTool_Definition(t *testing.T) {
	tool, _ := newPlanToolForTest(t)
	def := tool.Definition()

	if def.Function.Name != "RequestPlanApproval" {
		t.Errorf("expected name 'RequestPlanApproval', got %s", def.Function.Name)
	}
	if def.Function.Description == nil || *def.Function.Description == "" {
		t.Fatal("expected non-empty description")
	}

	params := def.Function.Parameters
	if params == nil {
		t.Fatal("expected non-nil parameters")
	}

	required, ok := (*params)["required"].([]string)
	if !ok {
		t.Fatalf("expected required to be []string, got %T", (*params)["required"])
	}
	wantRequired := map[string]bool{"title": false, "plan": false}
	for _, name := range required {
		if _, exists := wantRequired[name]; exists {
			wantRequired[name] = true
		}
	}
	for name, found := range wantRequired {
		if !found {
			t.Errorf("expected %q in required params, got %v", name, required)
		}
	}

	props, ok := (*params)["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected properties to be map[string]any, got %T", (*params)["properties"])
	}
	for _, name := range []string{"title", "plan"} {
		if _, exists := props[name]; !exists {
			t.Errorf("expected property %q in schema", name)
		}
	}
}

func TestRequestPlanApprovalTool_IsEnabled(t *testing.T) {
	tool, _ := newPlanToolForTest(t)
	if !tool.IsEnabled() {
		t.Error("expected RequestPlanApproval to be enabled by default")
	}
}

func TestRequestPlanApprovalTool_Validate(t *testing.T) {
	tool, _ := newPlanToolForTest(t)

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
		errSub  string
	}{
		{
			name:    "valid args",
			args:    map[string]any{"title": "My Plan", "plan": "## Context\n..."},
			wantErr: false,
		},
		{
			name:    "missing title",
			args:    map[string]any{"plan": "body"},
			wantErr: true,
			errSub:  "title",
		},
		{
			name:    "non-string title",
			args:    map[string]any{"title": 42, "plan": "body"},
			wantErr: true,
			errSub:  "title",
		},
		{
			name:    "empty title",
			args:    map[string]any{"title": "   ", "plan": "body"},
			wantErr: true,
			errSub:  "title",
		},
		{
			name:    "title with slash",
			args:    map[string]any{"title": "../etc/passwd", "plan": "body"},
			wantErr: true,
			errSub:  "path separators",
		},
		{
			name:    "title with backslash",
			args:    map[string]any{"title": `bad\title`, "plan": "body"},
			wantErr: true,
			errSub:  "path separators",
		},
		{
			name:    "title with parent traversal",
			args:    map[string]any{"title": "..", "plan": "body"},
			wantErr: true,
			errSub:  "path separators",
		},
		{
			name:    "missing plan",
			args:    map[string]any{"title": "Plan"},
			wantErr: true,
			errSub:  "plan",
		},
		{
			name:    "empty plan",
			args:    map[string]any{"title": "Plan", "plan": "   "},
			wantErr: true,
			errSub:  "plan",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSub)
				}
				if !strings.Contains(err.Error(), tt.errSub) {
					t.Errorf("expected error to contain %q, got %v", tt.errSub, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
		})
	}
}

func TestRequestPlanApprovalTool_Execute_WritesFile(t *testing.T) {
	tool, configDir := newPlanToolForTest(t)

	planBody := "## Context\n\nDo X.\n\n## Verification\n\nRun tests.\n"
	args := map[string]any{
		"title": "Add Feature X!",
		"plan":  planBody,
	}

	result, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}

	data, ok := result.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected Data to be map[string]any, got %T", result.Data)
	}
	gotPlan, _ := data["plan"].(string)
	if !strings.HasPrefix(gotPlan, "# Add Feature X!\n\n") {
		t.Errorf("expected plan in Data to be the file contents (H1 + body), got %q", gotPlan)
	}
	if !strings.Contains(gotPlan, strings.TrimSpace(planBody)) {
		t.Errorf("expected plan in Data to contain the input plan body, got %q", gotPlan)
	}
	if got, _ := data["title"].(string); got != "Add Feature X!" {
		t.Errorf("title in result Data does not match input, got %q", got)
	}
	path, _ := data["path"].(string)
	if path == "" {
		t.Fatal("expected non-empty path in result Data")
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected plan file to exist at %s, got %v", path, err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read plan file: %v", err)
	}
	got := string(content)
	if !strings.HasPrefix(got, "# Add Feature X!\n\n") {
		t.Errorf("expected file to start with H1 from title, got %q", got)
	}
	if !strings.Contains(got, "## Context") || !strings.Contains(got, "## Verification") {
		t.Errorf("expected file to contain plan section headings, got %q", got)
	}
	if got != gotPlan {
		t.Errorf("plan in Data should match file contents byte-for-byte:\nfile=%q\ndata=%q", got, gotPlan)
	}

	rel, err := filepath.Rel(configDir, path)
	if err != nil {
		t.Fatalf("failed to compute relative path: %v", err)
	}
	relParts := strings.Split(rel, string(filepath.Separator))
	if len(relParts) < 2 || relParts[0] != "plans" {
		t.Errorf("expected plan to be saved under 'plans/', got %s", rel)
	}

	filename := filepath.Base(path)
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-\d{6}-[a-z0-9-]+\.md$`)
	if !pattern.MatchString(filename) {
		t.Errorf("filename %q does not match expected pattern", filename)
	}
}

func TestRequestPlanApprovalTool_Execute_NoTempLeft(t *testing.T) {
	tool, configDir := newPlanToolForTest(t)

	result, err := tool.Execute(context.Background(), map[string]any{
		"title": "Atomic Write",
		"plan":  "body",
	})
	if err != nil || !result.Success {
		t.Fatalf("execute failed: err=%v success=%v error=%s", err, result.Success, result.Error)
	}

	plansDir := filepath.Join(configDir, "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		t.Fatalf("failed to read plans dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("found leftover .tmp file: %s", e.Name())
		}
	}
}

func TestRequestPlanApprovalTool_Execute_CreatesPlansDir(t *testing.T) {
	tool, configDir := newPlanToolForTest(t)

	plansDir := filepath.Join(configDir, "plans")
	if _, err := os.Stat(plansDir); !os.IsNotExist(err) {
		t.Fatalf("plans dir unexpectedly exists before Execute: %v", err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"title": "Create Dir",
		"plan":  "body",
	})
	if err != nil || !result.Success {
		t.Fatalf("execute failed: err=%v error=%s", err, result.Error)
	}

	info, err := os.Stat(plansDir)
	if err != nil {
		t.Fatalf("expected plans dir to be created, got %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected plans dir to be a directory")
	}
}

func TestRequestPlanApprovalTool_Execute_HandlesSlugCollision(t *testing.T) {
	tool, configDir := newPlanToolForTest(t)

	args := map[string]any{
		"title": "Same Title",
		"plan":  "body",
	}

	for i := range 3 {
		result, err := tool.Execute(context.Background(), args)
		if err != nil || !result.Success {
			t.Fatalf("iteration %d failed: err=%v error=%s", i, err, result.Error)
		}
	}

	entries, err := os.ReadDir(filepath.Join(configDir, "plans"))
	if err != nil {
		t.Fatalf("failed to read plans dir: %v", err)
	}
	mdFiles := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdFiles++
		}
	}
	if mdFiles != 3 {
		t.Errorf("expected 3 distinct plan files after collision handling, got %d", mdFiles)
	}
}

func TestSlugifyTitle(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"My Plan", "my-plan"},
		{"  Add Feature X!  ", "add-feature-x"},
		{"Already-slugged", "already-slugged"},
		{"!!!", "plan"},
		{strings.Repeat("a", 200), strings.Repeat("a", maxSlugLength)},
		{"unicode 文档 plan", "unicode-plan"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := slugifyTitle(tt.in)
			if got != tt.want {
				t.Errorf("slugifyTitle(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestBuildPlanMarkdown_AlwaysEndsWithNewline(t *testing.T) {
	got := buildPlanMarkdown("Title", "body without newline")
	if !strings.HasSuffix(got, "\n") {
		t.Error("expected output to end with newline")
	}
	if !strings.HasPrefix(got, "# Title\n\n") {
		t.Errorf("expected H1 prefix, got %q", got)
	}

	got2 := buildPlanMarkdown("Title", "body with newline\n")
	if strings.HasSuffix(got2, "\n\n") {
		t.Error("expected single trailing newline, not double")
	}
}
