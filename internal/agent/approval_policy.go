package agent

import (
	"context"
	"encoding/json"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
)

// StandardApprovalPolicy implements the default approval policy with the following rules:
//  1. Computer use tools (mouse, keyboard) always bypass approval (background execution)
//  2. Auto-accept mode bypasses all approval
//     2.5. ReadOnly mode (Explore-like subagent) bypasses approval; its toolset is
//     read-only by construction so nothing it can call mutates
//  3. Non-chat (headless agent) mode bypasses approval; there the Bash tool's own
//     per-mode gate (executeBash) decides what runs
//  4. Bash commands are governed by the per-mode allow-list (config.IsBashCommandAllowed):
//     reached only in chat, non-auto mode, so allowed commands bypass approval and
//     anything off-list prompts the user
//  5. Other tools check configuration (per-tool or global require_approval setting)
type StandardApprovalPolicy struct {
	config       *config.Config
	stateManager agentdomain.AgentModeManager
}

// NewStandardApprovalPolicy creates a new standard approval policy
func NewStandardApprovalPolicy(cfg *config.Config, stateManager agentdomain.AgentModeManager) *StandardApprovalPolicy {
	return &StandardApprovalPolicy{
		config:       cfg,
		stateManager: stateManager,
	}
}

// ShouldRequireApproval implements the approval decision logic
func (p *StandardApprovalPolicy) ShouldRequireApproval(
	ctx context.Context,
	toolCall *sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
) bool {
	if tools.IsComputerUseTool(toolCall.Function.Name) {
		return p.requiresComputerUseApproval(toolCall)
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == agentdomain.AgentModeAutoAccept {
		return false
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == agentdomain.AgentModeReadOnly {
		return false
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == agentdomain.AgentModePlan && !planModeAllowedTools[toolCall.Function.Name] {
		return false
	}

	if toolCall.Function.Name == "Bash" {
		return !p.isBashCommandAllowed(toolCall)
	}

	return p.config.IsApprovalRequired(toolCall.Function.Name)
}

// requiresComputerUseApproval checks whether a computer-use tool requires
// approval based on the computer_use.approval config setting. Unknown values
// fail closed: config load rejects them, but a config that bypassed
// validation must not silently disable a safety gate.
func (p *StandardApprovalPolicy) requiresComputerUseApproval(toolCall *sdk.ChatCompletionMessageToolCall) bool {
	switch p.config.ComputerUse.Approval {
	case config.ComputerUseApprovalNever, "":
		return false
	case config.ComputerUseApprovalDestructive:
		if toolCall.Function.Name != "Computer" {
			return false
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
			return true
		}
		action, _ := args["action"].(string)
		return action != "screenshot" && action != "cursor" && action != "accessibility"
	default:
		return true
	}
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

	return p.config.IsBashCommandAllowed(command, p.agentModeKey())
}

// agentModeKey resolves the bash allow-list mode key from the current agent mode,
// defaulting to standard when no state manager is wired.
func (p *StandardApprovalPolicy) agentModeKey() string {
	if p.stateManager != nil {
		return p.stateManager.GetAgentMode().AllowedlistKey()
	}
	return "standard"
}
