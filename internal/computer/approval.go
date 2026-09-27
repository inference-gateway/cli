package computer

import (
	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

// RequiresApproval gates the Computer tool per computer_use.approval: under
// "destructive" only input actions (click, type, key, ...) need approval,
// observations such as a screenshot do not.
func (t *ComputerTool) RequiresApproval(args map[string]any, _ agentdomain.AgentMode) bool {
	action, _ := args["action"].(string)
	return computerUseApproval(t.config, isObservation(actionKinds[action]))
}

// RequiresApproval gates GetLatestFrame per computer_use.approval; reading a
// frame is an observation.
func (t *GetLatestFrameTool) RequiresApproval(map[string]any, agentdomain.AgentMode) bool {
	return computerUseApproval(t.config, true)
}

// RequiresApproval gates RecordStart per computer_use.recording.require_approval
// (except in auto-accept mode) and both recording tools per
// computer_use.approval.
func (t *recordTool) RequiresApproval(_ map[string]any, mode agentdomain.AgentMode) bool {
	if !t.stop && t.config.ComputerUse.Recording.ApprovalRequired() && mode != agentdomain.AgentModeAutoAccept {
		return true
	}
	return computerUseApproval(t.config, true)
}

// NeedsSession reports that a recording outlives its call and must be
// finalized by the session that started it.
func (t *recordTool) NeedsSession() bool {
	return true
}

// computerUseApproval applies computer_use.approval. Unknown values fail
// closed: config load rejects them, but a config that bypassed validation
// must not silently disable a safety gate.
func computerUseApproval(cfg *config.Config, observation bool) bool {
	switch cfg.ComputerUse.Approval {
	case config.ComputerUseApprovalNever, "":
		return false
	case config.ComputerUseApprovalDestructive:
		return !observation
	default:
		return true
	}
}

func isObservation(kind computerdomain.ActionKind) bool {
	switch kind {
	case computerdomain.ActionScreenshot, computerdomain.ActionCursor, computerdomain.ActionAccessibility:
		return true
	}
	return false
}
