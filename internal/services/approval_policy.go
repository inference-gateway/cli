package services

import (
	"context"
	"encoding/json"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
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
	stateManager domain.AgentModeManager
}

// NewStandardApprovalPolicy creates a new standard approval policy
func NewStandardApprovalPolicy(cfg *config.Config, stateManager domain.AgentModeManager) *StandardApprovalPolicy {
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
	// Rule 1: Computer use tools always bypass approval (run silently)
	if tools.IsComputerUseTool(toolCall.Function.Name) {
		return false
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == domain.AgentModeAutoAccept {
		return false
	}

	if p.stateManager != nil && p.stateManager.GetAgentMode() == domain.AgentModeReadOnly {
		return false
	}

	if !isChatMode {
		return false
	}

	if toolCall.Function.Name == "Bash" {
		return !p.isBashCommandAllowed(toolCall)
	}

	return p.config.IsApprovalRequired(toolCall.Function.Name)
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

// PermissiveApprovalPolicy is an alternative policy that bypasses all approval
// Useful for automation, testing, or highly trusted environments
type PermissiveApprovalPolicy struct{}

// NewPermissiveApprovalPolicy creates a new permissive approval policy
func NewPermissiveApprovalPolicy() *PermissiveApprovalPolicy {
	return &PermissiveApprovalPolicy{}
}

// ShouldRequireApproval always returns false (no approval required)
func (p *PermissiveApprovalPolicy) ShouldRequireApproval(
	ctx context.Context,
	toolCall *sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
) bool {
	return false
}

// StrictApprovalPolicy is an alternative policy that requires approval for all tools
// Useful for security-sensitive environments or auditing
type StrictApprovalPolicy struct{}

// NewStrictApprovalPolicy creates a new strict approval policy
func NewStrictApprovalPolicy() *StrictApprovalPolicy {
	return &StrictApprovalPolicy{}
}

// ShouldRequireApproval always returns true (approval required for all tools)
// Exception: Computer use tools still bypass for UX reasons
func (p *StrictApprovalPolicy) ShouldRequireApproval(
	ctx context.Context,
	toolCall *sdk.ChatCompletionMessageToolCall,
	isChatMode bool,
) bool {
	// Still bypass approval for computer use tools (silent background execution)
	if tools.IsComputerUseTool(toolCall.Function.Name) {
		return false
	}
	return true
}
