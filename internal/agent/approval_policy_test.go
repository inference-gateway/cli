package agent

import (
	"context"
	"testing"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	tools "github.com/inference-gateway/cli/internal/tools"
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

// builtinTools resolves the built-in tools' manifests, which carry each
// tool's plan-mode policy and its default or configured approval.
func builtinTools(cfg *config.Config) *tools.Registry {
	return tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func newTestPolicy(cfg *config.Config, stateManager agentdomain.AgentModeState) *StandardApprovalPolicy {
	return NewStandardApprovalPolicy(cfg, stateManager, builtinTools(cfg))
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
	stateManager := statemanager.NewStore(false)
	stateManager.SetAgentMode(mode)
	return newTestPolicy(createTestConfig(), stateManager)
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
func approvalCases(prefix string, policy func(t *testing.T) agentdomain.ApprovalPolicy, args string, chat, want bool, toolNames ...string) []approvalCase {
	cases := make([]approvalCase, 0, len(toolNames))
	for _, tool := range toolNames {
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
			tool:   tools.ToolBash,
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
	tests = append(tests, approvalCases("auto-accept bypasses approval:", standardPolicy(agentdomain.AgentModeAutoAccept),
		`{"command": "rm -rf /"}`, true, false, tools.ToolBash, tools.ToolRead, tools.ToolWrite, tools.ToolEdit, tools.ToolGrep)...)
	tests = append(tests, approvalCases("read-only subagent bypasses approval in chat:", standardPolicy(agentdomain.AgentModeReadOnly),
		`{}`, true, false, tools.ToolRead, tools.ToolGrep, tools.ToolTree, tools.ToolWebFetch, tools.ToolWrite)...)
	tests = append(tests, approvalCases("non-chat follows the same approval rules:", standard, "{}", false, true,
		tools.ToolBash, tools.ToolRead, tools.ToolWrite, tools.ToolEdit)...)
	tests = append(tests, approvalCases("plan mode skips approval for exec-rejected tools:", standardPolicy(agentdomain.AgentModePlan),
		`{"command": "rm -rf /"}`, true, false, tools.ToolBash, tools.ToolWrite, tools.ToolEdit, tools.ToolDelete)...)
	judge := standardPolicy(agentdomain.AgentModeAutoWithJudge)
	tests = append(tests, approvalCases("judge mode follows standard rules:", judge,
		"{}", true, true, tools.ToolRead, tools.ToolWrite, tools.ToolEdit, tools.ToolGrep)...)
	tests = append(tests, bashCases("judge mode lets allowed bash bypass (no judge call):", judge, false,
		"ls", "pwd", "echo", "ls -la")...)
	tests = append(tests, bashCases("judge mode gates disallowed bash (one judge call):", judge, true,
		"rm -rf /", "sudo", "curl http://malicious.com")...)
	tests = append(tests, bashCases("allowed bash bypasses approval:", standard, false, "ls", "pwd", "echo", "ls -la")...)
	tests = append(tests, bashCases("disallowed bash requires approval:", standard, true, "rm -rf /", "sudo", "curl http://malicious.com")...)
	for _, args := range []string{`{}`, `{"command": 123}`, `invalid json`} {
		tests = append(tests, approvalCase{
			name: "invalid bash args require approval: " + args, policy: standard,
			tool: tools.ToolBash, args: args, chat: true, want: true,
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

// perCallTool is a tool outside the agent's built-ins that approves each call
// itself, the way computer use does.
const perCallTool = "PerCallTool"

type callApprovingTool struct {
	*agentdomainmocks.FakeTool
	*agentdomainmocks.FakeCallApprover
}

// newCallApproverPolicy builds a policy over the built-in tools plus
// perCallTool, whose approver answers requiresApproval.
func newCallApproverPolicy(mode agentdomain.AgentMode, requiresApproval bool) (*StandardApprovalPolicy, *agentdomainmocks.FakeCallApprover) {
	cfg := createTestConfig()
	approver := &agentdomainmocks.FakeCallApprover{}
	approver.RequiresApprovalReturns(requiresApproval)
	registry := builtinTools(cfg)
	registry.RegisterTools(map[string]agentdomain.Tool{
		perCallTool: callApprovingTool{&agentdomainmocks.FakeTool{}, approver},
	})
	stateManager := statemanager.NewStore(false)
	stateManager.SetAgentMode(mode)
	return NewStandardApprovalPolicy(cfg, stateManager, registry), approver
}

func TestStandardApprovalPolicy_CallApproverDecidesAheadOfMode(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []agentdomain.AgentMode{agentdomain.AgentModeAutoAccept, agentdomain.AgentModePlan, agentdomain.AgentModeReadOnly} {
		for _, want := range []bool{true, false} {
			policy, approver := newCallApproverPolicy(mode, want)
			if got := policy.ShouldRequireApproval(ctx, createToolCall(perCallTool, `{"action": "click"}`), false); got != want {
				t.Errorf("mode %s: ShouldRequireApproval = %v, want the approver's %v", mode, got, want)
			}
			args, gotMode := approver.RequiresApprovalArgsForCall(0)
			if args["action"] != "click" || gotMode != mode {
				t.Errorf("approver got (%v, %s), want the decoded arguments and mode %s", args, gotMode, mode)
			}
		}
	}
}

func TestStandardApprovalPolicy_ConfigBasedApproval(t *testing.T) {
	cfg := createTestConfig()
	stateManager := statemanager.NewStore(false)
	stateManager.SetAgentMode(agentdomain.AgentModeStandard)

	policy := newTestPolicy(cfg, stateManager)
	ctx := context.Background()

	for _, toolName := range []string{tools.ToolRead, tools.ToolWrite, tools.ToolEdit, tools.ToolGrep} {
		t.Run(toolName+" matches config", func(t *testing.T) {
			toolCall := createToolCall(toolName, "{}")

			requiresApproval := policy.ShouldRequireApproval(ctx, toolCall, true)
			configRequiresApproval := builtinTools(cfg).Manifest(toolName).RequiresApproval(cfg.Tools.Safety.RequireApproval)

			if requiresApproval != configRequiresApproval {
				t.Errorf("Expected %s approval requirement to match config: policy=%v, config=%v",
					toolName, requiresApproval, configRequiresApproval)
			}
		})
	}
}

func TestStandardApprovalPolicy_WithNilStateManager(t *testing.T) {
	policy := newTestPolicy(createTestConfig(), nil)
	ctx := context.Background()

	toolCall := createToolCall(tools.ToolRead, "{}")
	_ = policy.ShouldRequireApproval(ctx, toolCall, true)
}

func TestApprovalPolicy_PriorityOrder(t *testing.T) {
	t.Run("Rule priority: per-call approver > auto-accept > non-chat > bash allowedlist > config", func(t *testing.T) {
		ctx := context.Background()
		policy, _ := newCallApproverPolicy(agentdomain.AgentModeStandard, false)
		if policy.ShouldRequireApproval(ctx, createToolCall(perCallTool, "{}"), true) {
			t.Error("A per-call approver should bypass all other rules")
		}

		cfg := createTestConfig()
		stateManager := statemanager.NewStore(false)
		policy = newTestPolicy(cfg, stateManager)

		stateManager.SetAgentMode(agentdomain.AgentModeAutoAccept)
		bash := createToolCall(tools.ToolBash, `{"command": "rm -rf /"}`)
		if policy.ShouldRequireApproval(ctx, bash, true) {
			t.Error("Auto-accept mode should bypass bash allowedlist and config")
		}

		stateManager.SetAgentMode(agentdomain.AgentModeStandard)
		if !policy.ShouldRequireApproval(ctx, bash, false) {
			t.Error("Non-chat mode must enforce the bash allowedlist and config, not bypass them")
		}

		allowedBash := createToolCall(tools.ToolBash, `{"command": "ls"}`)
		if policy.ShouldRequireApproval(ctx, allowedBash, true) {
			t.Error("Allowed bash command should bypass config")
		}

		disallowedBash := createToolCall(tools.ToolBash, `{"command": "rm"}`)
		requiresApproval := policy.ShouldRequireApproval(ctx, disallowedBash, true)
		if !requiresApproval {
			t.Error("disallowed bash command should require approval based on config")
		}
	})
}
