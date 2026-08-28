package shortcuts

import (
	"context"
	"errors"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	config "github.com/inference-gateway/cli/config"
)

// stubShortcut stands in for a registered shortcut. The counterfeiter fake in
// tests/mocks/shortcuts imports this package, so it cannot be used from here.
type stubShortcut struct {
	name string
	res  ShortcutResult
}

func (s stubShortcut) GetName() string               { return s.name }
func (s stubShortcut) GetDescription() string        { return s.name + " description" }
func (s stubShortcut) GetUsage() string              { return "/" + s.name }
func (s stubShortcut) CanExecute(args []string) bool { return true }
func (s stubShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	return s.res, nil
}

func fake(name string, res ShortcutResult) Shortcut { return stubShortcut{name: name, res: res} }

func testRegistry(t *testing.T) *Registry {
	t.Helper()

	cfg := &config.Config{}
	cfg.Prompts.Init.Prompt = "write AGENTS.md"

	models := &convmocks.FakeModelService{}
	models.GetCurrentModelReturns("openai/gpt-4o")

	reg := NewRegistry()
	reg.Register(NewInitShortcut(cfg))
	reg.Register(NewExitShortcut())
	reg.Register(NewHelpShortcut(reg))
	reg.Register(NewCompactShortcut(&convmocks.FakeConversationRepository{}))
	reg.Register(NewSwitchShortcut(models))
	reg.Register(fake("report", ShortcutResult{Output: "all green", Success: true}))
	reg.Register(fake("panel", ShortcutResult{Success: true, SideEffect: SideEffectShowExplorer}))
	reg.Register(fake("broken", ShortcutResult{Output: "it broke", Success: false}))
	reg.Register(fake("voice", ShortcutResult{Success: true, SideEffect: SideEffectSetInput, Data: "transcript"}))
	return reg
}

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantHandled bool
		wantPrompt  string
		wantModel   string
		wantText    string
		wantErr     string
	}{
		{name: "plain prose is not a command", input: "fix issue #42"},
		{name: "unknown shortcut passes through", input: "/maintainer audit the repo"},
		{name: "file path passes through", input: "/tmp/notes.md summarize this"},
		{name: "voice never runs unattended", input: "/voice"},
		{name: "init becomes the prompt", input: "/init", wantHandled: true, wantPrompt: "write AGENTS.md"},
		{name: "surrounding whitespace is fine", input: "  /init  ", wantHandled: true, wantPrompt: "write AGENTS.md"},
		{name: "model prompt carries its model", input: "/model openai/gpt-4o explain this", wantHandled: true, wantPrompt: "explain this", wantModel: "openai/gpt-4o"},
		{name: "output is printed", input: "/report", wantHandled: true, wantText: "all green"},
		{name: "exit reports itself", input: "/exit", wantHandled: true, wantText: "Chat session ended"},
		{name: "help lists the registry", input: "/help", wantHandled: true, wantText: "/report - report description"},
		{name: "panel shortcut says so", input: "/panel", wantHandled: true, wantText: "opens a panel in the chat TUI"},
		{name: "failed shortcut errors", input: "/broken", wantHandled: true, wantErr: "shortcut /broken failed: it broke"},
		{name: "bad arguments error", input: "/init extra", wantHandled: true, wantErr: "shortcut /init failed"},
	}

	reg := testRegistry(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, handled, err := Run(context.Background(), reg, tt.input, Deps{})

			if handled != tt.wantHandled {
				t.Fatalf("Run(%q) handled = %v, want %v", tt.input, handled, tt.wantHandled)
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("Run(%q) error = %v, want it to contain %q", tt.input, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Run(%q) unexpected error: %v", tt.input, err)
			}
			if out.Prompt != tt.wantPrompt {
				t.Fatalf("Run(%q) prompt = %q, want %q", tt.input, out.Prompt, tt.wantPrompt)
			}
			if out.Model != tt.wantModel {
				t.Fatalf("Run(%q) model = %q, want %q", tt.input, out.Model, tt.wantModel)
			}
			if tt.wantText != "" && !strings.Contains(out.Text, tt.wantText) {
				t.Fatalf("Run(%q) text = %q, want it to contain %q", tt.input, out.Text, tt.wantText)
			}
		})
	}
}

func TestRunCompact(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetMessageCountReturns(4)
	reg := NewRegistry()
	reg.Register(NewCompactShortcut(repo))

	out, handled, err := Run(context.Background(), reg, "/compact", Deps{
		Compact: func(context.Context) (string, error) { return "summarised into sess-2", nil },
	})
	if err != nil || !handled || out.Text != "summarised into sess-2" {
		t.Fatalf("Run(/compact) = %+v, %v, %v; want the compaction result", out, handled, err)
	}

	out, _, err = Run(context.Background(), reg, "/compact", Deps{})
	if err != nil || !strings.Contains(out.Text, "needs a persistent conversation") {
		t.Fatalf("Run(/compact) without deps = %q, %v; want the unavailable notice", out.Text, err)
	}

	_, _, err = Run(context.Background(), reg, "/compact", Deps{
		Compact: func(context.Context) (string, error) { return "", errors.New("optimizer disabled") },
	})
	if err == nil || !strings.Contains(err.Error(), "optimizer disabled") {
		t.Fatalf("Run(/compact) error = %v, want the compaction failure", err)
	}
}

func TestRunNilRegistry(t *testing.T) {
	_, handled, err := Run(context.Background(), nil, "/init", Deps{})
	if handled || err != nil {
		t.Fatalf("Run(nil registry) = %v, %v; want not handled", handled, err)
	}
}

// stubRepo records the conversation switches /new and /clear perform. The
// counterfeiter fake for this interface lives in tests/mocks/shortcuts, which
// imports this package, so it is unusable from here.
type stubRepo struct {
	PersistentConversationRepository
	titles []string
}

func (r *stubRepo) StartNewConversation(title string) error {
	r.titles = append(r.titles, title)
	return nil
}

func TestRunStartsTheNewConversationItAnnounces(t *testing.T) {
	repo := &stubRepo{}
	reg := NewRegistry()
	reg.Register(NewNewShortcut(repo, nil))

	out, handled, err := Run(context.Background(), reg, "/new triage", Deps{})
	if err != nil || !handled {
		t.Fatalf("Run(/new) = %v, %v; want handled", handled, err)
	}
	if !strings.Contains(out.Text, "triage") {
		t.Fatalf("Run(/new) text = %q, want it to name the conversation", out.Text)
	}
	if len(repo.titles) != 1 || repo.titles[0] != "triage" {
		t.Fatalf("StartNewConversation calls = %v, want exactly [triage]", repo.titles)
	}
}
