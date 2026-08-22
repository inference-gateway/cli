package agent

import (
	"context"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

func createTestConfig() *config.Config {
	return &config.Config{
		Tools: config.ToolsConfig{
			Safety: config.SafetyConfig{
				RequireApproval: true,
			},
			Bash: config.BashToolConfig{
				Enabled: true,
				Mode: config.BashModesConfig{
					All: config.BashModeAllowConfig{Allow: []string{"ls( .*)?", "pwd( .*)?", "echo( .*)?"}},
				},
			},
		},
	}
}

func createToolCall(toolName string, args string) *sdk.ChatCompletionMessageToolCall {
	return &sdk.ChatCompletionMessageToolCall{
		ID:   "test-call-id",
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      toolName,
			Arguments: args,
		},
	}
}

// newStandardPolicy builds a StandardApprovalPolicy over a fresh test config
// with the state manager set to the given agent mode.
func newStandardPolicy(t *testing.T, mode agentdomain.AgentMode) agentdomain.ApprovalPolicy {
	t.Helper()
	stateManager := statemanager.NewStateManager(false)
	stateManager.SetAgentMode(mode)
	return NewStandardApprovalPolicy(createTestConfig(), stateManager)
}

type approvalCase struct {
	name   string
	policy func(t *testing.T) agentdomain.ApprovalPolicy
	tool   string
	args   string
	chat   bool
	want   bool
}

// approvalCases builds one case per tool, sharing the policy, args, chat flag,
// and expected outcome.
func approvalCases(prefix string, policy func(t *testing.T) agentdomain.ApprovalPolicy, args string, chat, want bool, tools ...string) []approvalCase {
	cases := make([]approvalCase, 0, len(tools))
	for _, tool := range tools {
		cases = append(cases, approvalCase{
			name:   prefix + " " + tool,
			policy: policy,
			tool:   tool,
			args:   args,
			chat:   chat,
			want:   want,
		})
	}
	return cases
}

func standardPolicy(mode agentdomain.AgentMode) func(t *testing.T) agentdomain.ApprovalPolicy {
	return func(t *testing.T) agentdomain.ApprovalPolicy { return newStandardPolicy(t, mode) }
}

// bashCases builds one Bash case per command with the given expectation.
func bashCases(prefix string, policy func(t *testing.T) agentdomain.ApprovalPolicy, want bool, commands ...string) []approvalCase {
	cases := make([]approvalCase, 0, len(commands))
	for _, cmd := range commands {
		cases = append(cases, approvalCase{
			name:   prefix + " " + cmd,
			policy: policy,
			tool:   "Bash",
			args:   `{"command": "` + cmd + `"}`,
			chat:   true,
			want:   want,
		})
	}
	return cases
}

func buildApprovalCases() []approvalCase {
	standard := standardPolicy(agentdomain.AgentModeStandard)
	var tests []approvalCase
	tests = append(tests, approvalCases("computer use bypasses approval:", standard, `{"action": "click", "x": 1, "y": 1}`, true, false,
		"Computer", "GetLatestFrame")...)
	tests = append(tests, approvalCases("auto-accept bypasses approval:", standardPolicy(agentdomain.AgentModeAutoAccept),
		`{"command": "rm -rf /"}`, true, false, "Bash", "Read", "Write", "Edit", "Grep")...)
	tests = append(tests, approvalCases("read-only subagent bypasses approval in chat:", standardPolicy(agentdomain.AgentModeReadOnly),
		`{}`, true, false, "Read", "Grep", "Tree", "WebFetch", "Write")...)
	tests = append(tests, approvalCases("non-chat follows the same approval rules:", standard, "{}", false, true,
		"Bash", "Read", "Write", "Edit")...)
	tests = append(tests, bashCases("allowed bash bypasses approval:", standard, false, "ls", "pwd", "echo", "ls -la")...)
	tests = append(tests, bashCases("disallowed bash requires approval:", standard, true, "rm -rf /", "sudo", "curl http://malicious.com")...)
	for _, args := range []string{`{}`, `{"command": 123}`, `invalid json`} {
		tests = append(tests, approvalCase{
			name: "invalid bash args require approval: " + args, policy: standard,
			tool: "Bash", args: args, chat: true, want: true,
		})
	}
	return tests
}

func TestApprovalPolicies_ShouldRequireApproval(t *testing.T) {
	ctx := context.Background()
	for _, tt := range buildApprovalCases() {
		t.Run(tt.name, func(t *testing.T) {
			policy := tt.policy(t)
			got := policy.ShouldRequireApproval(ctx, createToolCall(tt.tool, tt.args), tt.chat)
			if got != tt.want {
				t.Errorf("ShouldRequireApproval(%s, %s, chat=%v) = %v, want %v", tt.tool, tt.args, tt.chat, got, tt.want)
			}
		})
	}
}

func TestStandardApprovalPolicy_ComputerUseApprovalLevels(t *testing.T) {
	ctx := context.Background()
	click := `{"action": "click", "x": 1, "y": 1}`
	screenshot := `{"action": "screenshot"}`
	tests := []struct {
		name     string
		approval string
		tool     string
		args     string
		want     bool
	}{
		{"never bypasses destructive action", config.ComputerUseApprovalNever, "Computer", click, false},
		{"empty bypasses destructive action", "", "Computer", click, false},
		{"destructive gates click", config.ComputerUseApprovalDestructive, "Computer", click, true},
		{"destructive gates type", config.ComputerUseApprovalDestructive, "Computer", `{"action": "type", "text": "hi"}`, true},
		{"destructive bypasses screenshot", config.ComputerUseApprovalDestructive, "Computer", screenshot, false},
		{"destructive bypasses cursor", config.ComputerUseApprovalDestructive, "Computer", `{"action": "cursor"}`, false},
		{"destructive bypasses accessibility", config.ComputerUseApprovalDestructive, "Computer", `{"action": "accessibility"}`, false},
		{"destructive gates accessibility press", config.ComputerUseApprovalDestructive, "Computer", `{"action": "press", "label": "Save"}`, true},
		{"destructive bypasses GetLatestFrame", config.ComputerUseApprovalDestructive, "GetLatestFrame", "{}", false},
		{"always gates screenshot", config.ComputerUseApprovalAlways, "Computer", screenshot, true},
		{"unknown value fails closed", "alway", "Computer", screenshot, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := createTestConfig()
			cfg.ComputerUse.Approval = tt.approval
			stateManager := statemanager.NewStateManager(false)
			stateManager.SetAgentMode(agentdomain.AgentModeStandard)
			policy := NewStandardApprovalPolicy(cfg, stateManager)
			if got := policy.ShouldRequireApproval(ctx, createToolCall(tt.tool, tt.args), true); got != tt.want {
				t.Errorf("ShouldRequireApproval(%s, %s) with approval=%q = %v, want %v", tt.tool, tt.args, tt.approval, got, tt.want)
			}
		})
	}
}

func TestStandardApprovalPolicy_ConfigBasedApproval(t *testing.T) {
	cfg := createTestConfig()
	stateManager := statemanager.NewStateManager(false)
	stateManager.SetAgentMode(agentdomain.AgentModeStandard)

	policy := NewStandardApprovalPolicy(cfg, stateManager)
	ctx := context.Background()

	for _, toolName := range []string{"Read", "Write", "Edit", "Grep"} {
		t.Run(toolName+" matches config", func(t *testing.T) {
			toolCall := createToolCall(toolName, "{}")

			requiresApproval := policy.ShouldRequireApproval(ctx, toolCall, true)
			configRequiresApproval := cfg.IsApprovalRequired(toolName)

			if requiresApproval != configRequiresApproval {
				t.Errorf("Expected %s approval requirement to match config: policy=%v, config=%v",
					toolName, requiresApproval, configRequiresApproval)
			}
		})
	}
}

func TestStandardApprovalPolicy_WithNilStateManager(t *testing.T) {
	policy := NewStandardApprovalPolicy(createTestConfig(), nil)
	ctx := context.Background()

	toolCall := createToolCall("Read", "{}")
	_ = policy.ShouldRequireApproval(ctx, toolCall, true)
}

func TestApprovalPolicy_PriorityOrder(t *testing.T) {
	t.Run("Rule priority: computer use > auto-accept > non-chat > bash allowedlist > config", func(t *testing.T) {
		cfg := createTestConfig()
		stateManager := statemanager.NewStateManager(false)
		ctx := context.Background()

		stateManager.SetAgentMode(agentdomain.AgentModeStandard)
		policy := NewStandardApprovalPolicy(cfg, stateManager)

		computerClick := createToolCall("Computer", `{"action": "click", "x": 1, "y": 1}`)
		if policy.ShouldRequireApproval(ctx, computerClick, true) {
			t.Error("Computer use tool should bypass all other rules")
		}

		stateManager.SetAgentMode(agentdomain.AgentModeAutoAccept)
		bash := createToolCall("Bash", `{"command": "rm -rf /"}`)
		if policy.ShouldRequireApproval(ctx, bash, true) {
			t.Error("Auto-accept mode should bypass bash allowedlist and config")
		}

		stateManager.SetAgentMode(agentdomain.AgentModeStandard)
		if !policy.ShouldRequireApproval(ctx, bash, false) {
			t.Error("Non-chat mode must enforce the bash allowedlist and config, not bypass them")
		}

		allowedBash := createToolCall("Bash", `{"command": "ls"}`)
		if policy.ShouldRequireApproval(ctx, allowedBash, true) {
			t.Error("Allowed bash command should bypass config")
		}

		disallowedBash := createToolCall("Bash", `{"command": "rm"}`)
		requiresApproval := policy.ShouldRequireApproval(ctx, disallowedBash, true)
		if !requiresApproval {
			t.Error("disallowed bash command should require approval based on config")
		}
	})
}
