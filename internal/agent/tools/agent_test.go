package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	agentrunner "github.com/inference-gateway/cli/internal/services/agentrunner"
	utils "github.com/inference-gateway/cli/internal/utils"
)

func newTestAgentTool(t *testing.T) *AgentTool {
	t.Helper()
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "headless"
	return NewAgentTool(cfg, utils.NewSubagentTracker(), nil)
}

func TestAgentTool_Definition(t *testing.T) {
	def := newTestAgentTool(t).Definition()
	if def.Function.Name != "Agent" {
		t.Fatalf("Definition name = %q, want Agent", def.Function.Name)
	}
}

func TestAgentTool_Validate(t *testing.T) {
	tool := newTestAgentTool(t)
	if err := tool.Validate(map[string]any{}); err == nil {
		t.Fatalf("expected error when neither tasks nor description provided")
	}
	if err := tool.Validate(map[string]any{"description": "do x"}); err != nil {
		t.Fatalf("single description should validate: %v", err)
	}
	if err := tool.Validate(map[string]any{"tasks": []any{map[string]any{"description": "x"}}}); err != nil {
		t.Fatalf("tasks array should validate: %v", err)
	}
	if err := tool.Validate(map[string]any{"tasks": []any{map[string]any{"label": "no-desc"}}}); err == nil {
		t.Fatalf("task without description should fail validation")
	}
}

func TestAgentTool_DepthCapDisables(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "1")
	tool := NewAgentTool(config.DefaultConfig(), utils.NewSubagentTracker(), nil)
	if tool.IsEnabled() {
		t.Fatalf("Agent tool must disable itself at depth >= max_depth")
	}
}

func TestAgentTool_SyncFanOut(t *testing.T) {
	tool := newTestAgentTool(t)
	var mu sync.Mutex
	var calls int
	tool.runHeadless = func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return agentrunner.Result{FinalAssistant: "answer:" + opts.Prompt}, nil
	}

	args := map[string]any{
		"tasks": []any{
			map[string]any{"description": "task A", "label": "A"},
			map[string]any{"description": "task B", "label": "B"},
		},
	}
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	data, ok := res.Data.(AgentToolResult)
	if !ok {
		t.Fatalf("unexpected data type %T", res.Data)
	}
	if data.Dispatched != 2 || len(data.Subagents) != 2 {
		t.Fatalf("dispatched=%d subagents=%d, want 2/2", data.Dispatched, len(data.Subagents))
	}
	if calls != 2 {
		t.Fatalf("runHeadless calls = %d, want 2", calls)
	}
	for _, s := range data.Subagents {
		if !s.Success || s.Result == "" {
			t.Fatalf("subagent result not harvested: %+v", s)
		}
	}
}

func TestAgentTool_InteractiveFallsBackToHeadless(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "interactive" // mode is config-driven, not an LLM arg
	tool := NewAgentTool(cfg, utils.NewSubagentTracker(), nil)
	tool.interactiveAvailable = func() bool { return false }
	tool.launchPane = func(ctx context.Context, title, command string) (string, error) {
		t.Fatalf("tmux pane must not be launched when falling back to headless")
		return "", nil
	}
	var headlessUsed bool
	tool.runHeadless = func(ctx context.Context, opts agentrunner.Options) (agentrunner.Result, error) {
		headlessUsed = true
		return agentrunner.Result{FinalAssistant: "ok"}, nil
	}

	args := map[string]any{"description": "do x"}
	if _, err := tool.Execute(context.Background(), args); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !headlessUsed {
		t.Fatalf("expected interactive mode to fall back to headless when not in tmux")
	}
}

func TestAgentTool_InteractiveErrorFallback(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "interactive"
	cfg.Tools.Agent.Interactive.Fallback = "error"
	tool := NewAgentTool(cfg, utils.NewSubagentTracker(), nil)
	tool.interactiveAvailable = func() bool { return false }

	args := map[string]any{"description": "do x"}
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatalf("expected failure when interactive requested outside tmux with fallback=error")
	}
}

func TestParseAgentTasks(t *testing.T) {
	specs, err := parseAgentTasks(map[string]any{"description": "only one"})
	if err != nil || len(specs) != 1 || specs[0].Description != "only one" {
		t.Fatalf("single description parse failed: specs=%+v err=%v", specs, err)
	}

	specs, err = parseAgentTasks(map[string]any{"tasks": []any{
		map[string]any{"description": "a", "label": "la", "model": "m1", "system_prompt": "be terse"},
		map[string]any{"description": "b"},
	}})
	if err != nil || len(specs) != 2 {
		t.Fatalf("tasks parse failed: specs=%+v err=%v", specs, err)
	}
	if specs[0].Label != "la" || specs[0].Model != "m1" || specs[0].SystemPrompt != "be terse" {
		t.Fatalf("task fields not parsed: %+v", specs[0])
	}

	if _, err := parseAgentTasks(map[string]any{}); err == nil {
		t.Fatalf("expected error for empty args")
	}
}

func TestBuildChatPaneCommand_EmitsSubagentMode(t *testing.T) {
	tool := newTestAgentTool(t)

	if got := tool.buildChatPaneCommand(AgentTaskSpec{Mode: domain.AgentModeReadOnly}, "sess"); !strings.Contains(got, "INFER_SUBAGENT_AGENT_MODE='readonly'") {
		t.Fatalf("ReadOnly subagent must emit readonly mode; cmd = %q", got)
	}
	if got := tool.buildChatPaneCommand(AgentTaskSpec{Mode: domain.AgentModeStandard}, "sess"); strings.Contains(got, "INFER_SUBAGENT_AGENT_MODE") {
		t.Fatalf("ReadWrite (Standard) subagent must NOT emit the mode var; cmd = %q", got)
	}
}

// buildChatPaneCommand must tell the subagent's chat where to write its last
// assistant message so the parent can harvest the real result (not pane chrome).
func TestBuildChatPaneCommand_PassesResultFile(t *testing.T) {
	tool := newTestAgentTool(t)
	got := tool.buildChatPaneCommand(AgentTaskSpec{}, "sess-xyz")
	if !strings.Contains(got, "INFER_SUBAGENT_RESULT_FILE=") || !strings.Contains(got, "infer-subagent-sess-xyz.json") {
		t.Fatalf("expected the result-file env var for the session; cmd = %q", got)
	}
}

// buildChatPaneCommand must slugify the LLM-supplied label into a safe, dashcase
// history name (no spaces, path separators, or traversal) and fall back to the
// memory-only sentinel when there is no usable label - never the per-spawn session id.
func TestBuildChatPaneCommand_SlugifiesHistoryName(t *testing.T) {
	tool := newTestAgentTool(t)

	if got := tool.buildChatPaneCommand(AgentTaskSpec{Label: "Refactor Auth"}, "sess"); !strings.Contains(got, "INFER_SUBAGENT_HISTORY_NAME='refactor-auth'") {
		t.Fatalf("label must be slugified to dashcase; cmd = %q", got)
	}
	if got := tool.buildChatPaneCommand(AgentTaskSpec{Label: "a/../../../tmp/pwned"}, "sess"); !strings.Contains(got, "INFER_SUBAGENT_HISTORY_NAME='a-tmp-pwned'") {
		t.Fatalf("path separators/traversal must be sanitized out; cmd = %q", got)
	}
	if got := tool.buildChatPaneCommand(AgentTaskSpec{}, "subagent-parent-uuid"); !strings.Contains(got, "INFER_SUBAGENT_HISTORY_NAME='"+domain.SubagentHistoryMemoryOnly+"'") {
		t.Fatalf("unlabeled subagent must use the memory-only sentinel, not the session id; cmd = %q", got)
	}
}

func TestSubagentExtraEnv_EmitsSubagentMode(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	if env := strings.Join(subagentExtraEnv(AgentTaskSpec{Mode: domain.AgentModeReadOnly}), " "); !strings.Contains(env, "INFER_SUBAGENT_AGENT_MODE=readonly") {
		t.Fatalf("ReadOnly subagent must add the readonly mode var to headless env; got %q", env)
	}
	if env := strings.Join(subagentExtraEnv(AgentTaskSpec{Mode: domain.AgentModeStandard}), " "); strings.Contains(env, "INFER_SUBAGENT_AGENT_MODE") {
		t.Fatalf("ReadWrite (Standard) subagent must NOT add the mode var; got %q", env)
	}
}

// resolveSubagentType maps the `type` argument to a capability mode, defaulting
// to ReadOnly; the parent's own mode no longer leaks into the subagent.
func TestResolveSubagentType(t *testing.T) {
	cases := map[string]domain.AgentMode{
		"":          domain.AgentModeReadOnly,
		"ReadOnly":  domain.AgentModeReadOnly,
		"readonly":  domain.AgentModeReadOnly,
		"ReadWrite": domain.AgentModeStandard,
		"readwrite": domain.AgentModeStandard,
		"bogus":     domain.AgentModeReadOnly,
	}
	for in, want := range cases {
		if got := resolveSubagentType(in); got != want {
			t.Fatalf("resolveSubagentType(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestAgentTool_InteractiveDefaultsToReadOnly exercises the full Execute path: a
// subagent with no `type` defaults to ReadOnly, and the parent's mode (here
// AutoAccept) must NOT leak into it - capability is type-driven, not inherited.
func TestAgentTool_InteractiveDefaultsToReadOnly(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "interactive"
	cfg.Tools.Agent.Wait = true
	tool := NewAgentTool(cfg, utils.NewSubagentTracker(), nil)
	tool.interactiveAvailable = func() bool { return true }
	var captured string
	tool.launchPane = func(ctx context.Context, title, command string) (string, error) {
		captured = command
		return "", nil
	}

	ctx := domain.WithAgentMode(context.Background(), domain.AgentModeAutoAccept)
	if _, err := tool.Execute(ctx, map[string]any{"description": "do x"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(captured, "INFER_SUBAGENT_AGENT_MODE='readonly'") {
		t.Fatalf("subagent must default to readonly regardless of parent mode; cmd = %q", captured)
	}

	if _, err := tool.Execute(ctx, map[string]any{"description": "do x", "type": "ReadWrite"}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(captured, "INFER_SUBAGENT_AGENT_MODE") {
		t.Fatalf("ReadWrite subagent must run as Standard (no mode var); cmd = %q", captured)
	}
}
