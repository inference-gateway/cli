package tools

import (
	"context"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentrunner "github.com/inference-gateway/cli/internal/agent/runner"
	schedinfra "github.com/inference-gateway/cli/internal/scheduler/infrastructure"
)

// namedAgentTool returns a headless-mode tool with one loaded Markdown agent.
func namedAgentTool(t *testing.T) *AgentTool {
	t.Helper()
	tool := newTestAgentTool(t)
	tool.setMarkdownAgents([]markdownAgent{{
		name:         "code-reviewer",
		description:  "Reviews a diff for correctness bugs.",
		model:        "deepseek/deepseek-v4-pro",
		tools:        []string{ToolGrep, ToolRead},
		systemPrompt: "You are a senior reviewer.",
	}, {
		name:        "all-readonly",
		description: "Only read-only tools.",
		tools:       []string{ToolRead, ToolGrep, ToolTree},
	}, {
		name:        "editor",
		description: "Reads and edits files.",
		tools:       []string{ToolRead, ToolEdit},
	}, {
		name:        "unrestricted",
		description: "No tool restriction.",
	}}, toolManifests)
	return tool
}

func TestAgentTool_NamedAgentHeadlessPreset(t *testing.T) {
	tool := namedAgentTool(t)
	var opts agentrunner.Options
	tool.runHeadless = func(ctx context.Context, o agentrunner.Options) (agentrunner.Result, error) {
		opts = o
		return agentrunner.Result{FinalAssistant: "ok"}, nil
	}

	res, err := tool.Execute(context.Background(), map[string]any{"description": "review the diff", "agent": "code-reviewer"})
	if err != nil || !res.Success {
		t.Fatalf("Execute failed: err=%v res=%+v", err, res)
	}
	env := strings.Join(opts.ExtraEnv, "; ")
	if !strings.Contains(env, "INFER_SUBAGENT_SYSTEM_PROMPT=You are a senior reviewer.") {
		t.Fatalf("file body must become the system prompt; env = %q", env)
	}
	if !strings.Contains(env, "INFER_SUBAGENT_TOOLS=Grep,Read") {
		t.Fatalf("resolved allowlist must reach the child; env = %q", env)
	}
	if opts.Model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("frontmatter model must win, got %q", opts.Model)
	}
}

func TestAgentTool_NamedAgentModelPrecedence(t *testing.T) {
	tool := namedAgentTool(t)
	tool.config.Tools.Agent.Model = "configured-model"

	var model string
	tool.runHeadless = func(ctx context.Context, o agentrunner.Options) (agentrunner.Result, error) {
		model = o.Model
		return agentrunner.Result{FinalAssistant: "ok"}, nil
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "code-reviewer", "model": "per-task-model"}); err != nil {
		t.Fatal(err)
	}
	if model != "deepseek/deepseek-v4-pro" {
		t.Fatalf("file model must beat per-task model and config, got %q", model)
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "unrestricted", "model": "per-task-model"}); err != nil {
		t.Fatal(err)
	}
	if model != "per-task-model" {
		t.Fatalf("no frontmatter model must fall through to the per-task model, got %q", model)
	}
}

func TestAgentTool_NamedAgentModeDerived(t *testing.T) {
	tool := namedAgentTool(t)
	tool.config.Tools.Agent.Wait = true
	var env string
	tool.runHeadless = func(ctx context.Context, o agentrunner.Options) (agentrunner.Result, error) {
		env = strings.Join(o.ExtraEnv, "; ")
		return agentrunner.Result{FinalAssistant: "ok"}, nil
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "all-readonly", "type": "ReadWrite"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(env, "INFER_SUBAGENT_AGENT_MODE=readonly") {
		t.Fatalf("all read-only allowlist must derive ReadOnly mode; env = %q", env)
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "editor"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env, "INFER_SUBAGENT_AGENT_MODE") {
		t.Fatalf("mutating tool in allowlist must derive ReadWrite (no mode var); env = %q", env)
	}
	if !strings.Contains(env, "INFER_SUBAGENT_TOOLS=Read,Edit") {
		t.Fatalf("derived allowlist must reach the child; env = %q", env)
	}

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "unrestricted"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env, "INFER_SUBAGENT_TOOLS=") {
		t.Fatalf("unrestricted named agent must not restrict tools; env = %q", env)
	}
	if strings.Contains(env, "INFER_SUBAGENT_AGENT_MODE") {
		t.Fatalf("no allowlist must run as ReadWrite (no mode var); env = %q", env)
	}
}

func TestAgentTool_UnknownNamedAgentFails(t *testing.T) {
	tool := namedAgentTool(t)
	res, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "does-not-exist"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatalf("unknown agent must fail the call: %+v", res)
	}
	if !strings.Contains(res.Error, "available agents") || !strings.Contains(res.Error, "code-reviewer") {
		t.Fatalf("error must list available agents, got %q", res.Error)
	}
}

func TestAgentTool_NamedAgentInteractivePaneCommand(t *testing.T) {
	t.Setenv("INFER_SUBAGENT_DEPTH", "")
	cfg := config.DefaultConfig()
	cfg.Tools.Agent.Mode = "interactive"
	tool := NewAgentTool(cfg, schedinfra.NewSubagentTracker(), nil)
	tool.setMarkdownAgents([]markdownAgent{{
		name:         "code-reviewer",
		description:  "Reviews a diff for correctness bugs.",
		model:        "deepseek/deepseek-v4-pro",
		tools:        []string{ToolGrep, ToolRead},
		systemPrompt: "You are a senior reviewer.",
	}}, toolManifests)
	tool.interactiveAvailable = func() bool { return true }
	var cmd string
	tool.launchPane = func(ctx context.Context, title, command string) (string, error) {
		cmd = command
		return "pane1", nil
	}
	tool.sendTask = func(ctx context.Context, paneID, task string) error { return nil }

	if _, err := tool.Execute(context.Background(), map[string]any{"description": "d", "agent": "code-reviewer"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cmd, "INFER_SUBAGENT_TOOLS='Grep,Read'") {
		t.Fatalf("pane command must carry the allowlist; cmd = %q", cmd)
	}
	if !strings.Contains(cmd, "INFER_AGENT_MODEL='deepseek/deepseek-v4-pro'") {
		t.Fatalf("pane command must carry the frontmatter model; cmd = %q", cmd)
	}
	if !strings.Contains(cmd, "INFER_SUBAGENT_SYSTEM_PROMPT='You are a senior reviewer.'") {
		t.Fatalf("pane command must carry the file body as system prompt; cmd = %q", cmd)
	}
}

func TestAgentTool_DefinitionListsAvailableAgents(t *testing.T) {
	tool := namedAgentTool(t)
	desc := *tool.Definition().Function.Description
	if !strings.Contains(desc, "Available agents:") || !strings.Contains(desc, "- code-reviewer: Reviews a diff for correctness bugs.") {
		t.Fatalf("definition must list loaded agents: %q", desc)
	}
	props := (*tool.Definition().Function.Parameters)["properties"].(map[string]any)
	if _, ok := props["agent"]; !ok {
		t.Fatalf("definition must expose the agent property")
	}
	if _, ok := props["tasks"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["agent"]; !ok {
		t.Fatalf("task items must expose the agent property")
	}

	plain := newTestAgentTool(t)
	if strings.Contains(*plain.Definition().Function.Description, "Available agents:") {
		t.Fatalf("definition must stay unchanged with no agents loaded")
	}
}

func TestAgentTool_NamedAgentDefaults(t *testing.T) {
	tool := namedAgentTool(t)
	specs, err := parseAgentTasks(map[string]any{"description": "d", "agent": "unrestricted"})
	if err != nil {
		t.Fatal(err)
	}
	if specs[0].Agent != "unrestricted" {
		t.Fatalf("agent argument not parsed: %+v", specs[0])
	}
	if err := tool.applyNamedAgent(&specs[0]); err != nil {
		t.Fatal(err)
	}
	if specs[0].Mode != agentdomain.AgentModeStandard {
		t.Fatalf("named agent without tools must run as ReadWrite, got %v", specs[0].Mode)
	}
	if specs[0].Tools != nil {
		t.Fatalf("named agent without tools must inherit the parent's tools, got %v", specs[0].Tools)
	}
}
