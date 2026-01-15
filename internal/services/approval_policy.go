package services

import (
	"context"
	"encoding/json"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	tools "github.com/inference-gateway/cli/internal/services/tools"
)

// StandardApprovalPolicy implements the default approval policy with the following rules:
// 1. Computer use tools (mouse, keyboard) always bypass approval (background execution)
// 2. Auto-accept mode bypasses all approval
// 3. Non-chat mode bypasses approval
// 4. Bash commands check whitelist (whitelisted commands bypass approval)
// 5. Other tools check configuration (per-tool or global require_approval setting)
type StandardApprovalPolicy struct {
	config       *config.Config
	stateManager domain.StateManager
}

// NewStandardApprovalPolicy creates a new standard approval policy
func NewStandardApprovalPolicy(cfg *config.Config, stateManager domain.StateManager) *StandardApprovalPolicy {
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

	// Rule 2: Auto-accept mode bypasses all approval
	if p.stateManager != nil && p.stateManager.GetAgentMode() == domain.AgentModeAutoAccept {
		return false
	}

	// Rule 3: Non-chat mode bypasses approval
	if !isChatMode {
		return false
	}

	// Rule 4: Bash commands check whitelist
	if toolCall.Function.Name == "Bash" {
		return !p.isBashCommandWhitelisted(toolCall)
	}

	// Rule 5: Check configuration (per-tool or global setting)
	return p.config.IsApprovalRequired(toolCall.Function.Name)
}

// isBashCommandWhitelisted checks if a Bash tool command is in the whitelist
func (p *StandardApprovalPolicy) isBashCommandWhitelisted(toolCall *sdk.ChatCompletionMessageToolCall) bool {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return false
	}

	command, ok := args["command"].(string)
	if !ok {
		return false
	}

	return p.config.IsBashCommandWhitelisted(command)
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
