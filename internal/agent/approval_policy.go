package agent

import (
	"context"
	"encoding/json"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// StandardApprovalPolicy implements the default approval policy with the following rules:
//  1. A tool that approves per call (agentdomain.CallApprover) decides for
//     itself, ahead of the agent mode
//  2. Auto-accept mode bypasses all approval
//     2.5. ReadOnly mode (Explore-like subagent) bypasses approval; its toolset is
//     read-only by construction so nothing it can call mutates. Neither needs
//     a call the mode does not make available: execution rejects it
//  3. Non-chat (headless agent) mode bypasses approval; there the Bash tool's own
//     per-mode gate (executeBash) decides what runs
//  4. Bash commands are governed by the per-mode allow-list (sandbox.IsBashCommandAllowed):
//     reached only in chat, non-auto mode, so allowed commands bypass approval and
//     anything off-list prompts the user
//  5. Other tools follow their own require_approval setting, then their
//     manifest's default, then the global require_approval setting
type StandardApprovalPolicy struct {
	config       *config.Config
	stateManager agentdomain.AgentModeState
	tools        ApprovalTools
}

// ApprovalTools is what the approval policy needs to know about the tools: a
// tool's manifest, and the tool itself when it approves per call.
type ApprovalTools interface {
	agentdomain.ToolManifestLookup
	GetTool(name string) (agentdomain.Tool, error)
}

// NewStandardApprovalPolicy creates a new standard approval policy. tools
// resolves each tool's manifest for its plan-mode and approval defaults; with
// nil every tool gets the default policy.
func NewStandardApprovalPolicy(cfg *config.Config, stateManager agentdomain.AgentModeState, tools ApprovalTools) *StandardApprovalPolicy {
	return &StandardApprovalPolicy{
		config:       cfg,
		stateManager: stateManager,
		tools:        tools,
	}
}

// ShouldRequireApproval implements the approval decision logic
func (p *StandardApprovalPolicy) ShouldRequireApproval(
	ctx context.Context,
	toolCall *sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
) bool {
	if approver, ok := p.callApprover(toolCall.Function.Name); ok {
		return approver.RequiresApproval(callArguments(toolCall), p.agentMode())
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == agentdomain.AgentModeAutoAccept {
		return false
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == agentdomain.AgentModeReadOnly {
		return false
	}

	manifest := p.manifest(toolCall.Function.Name)
	if !manifest.AvailableIn(p.agentMode()) {
		return false
	}

	if toolCall.Function.Name == tools.ToolBash {
		return !p.isBashCommandAllowed(toolCall)
	}

	return manifest.RequiresApproval(p.config.Tools.Safety.RequireApproval)
}

func (p *StandardApprovalPolicy) manifest(toolName string) agentdomain.ToolManifest {
	if p.tools == nil {
		return agentdomain.ToolManifest{Name: toolName}
	}
	return p.tools.Manifest(toolName)
}

func (p *StandardApprovalPolicy) callApprover(toolName string) (agentdomain.CallApprover, bool) {
	if p.tools == nil {
		return nil, false
	}
	tool, err := p.tools.GetTool(toolName)
	if err != nil {
		return nil, false
	}
	approver, ok := tool.(agentdomain.CallApprover)
	return approver, ok
}

// callArguments decodes a call's JSON arguments; malformed arguments decode
// to none, which a per-call approver treats as its most cautious case.
func callArguments(toolCall *sdk.ChatCompletionMessageToolCall) map[string]any {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return nil
	}
	return args
}

// isBashCommandAllowed checks whether a Bash tool call's command is auto-approved
// for the active agent mode via the per-mode allow-list.
func (p *StandardApprovalPolicy) isBashCommandAllowed(toolCall *sdk.ChatCompletionMessageToolCall) bool {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return false
	}

	command, ok := args["command"].(string)
	if !ok {
		return false
	}

	return sandbox.IsBashCommandAllowed(p.config, command, p.agentMode())
}

// agentMode resolves the current agent mode, defaulting to standard when no
// state manager is wired.
func (p *StandardApprovalPolicy) agentMode() agentdomain.AgentMode {
	if p.stateManager != nil {
		return p.stateManager.GetAgentMode()
	}
	return agentdomain.AgentModeStandard
}
