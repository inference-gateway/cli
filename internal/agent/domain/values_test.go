package domain

import (
	"fmt"
	"testing"
)

func TestEnumStrings(t *testing.T) {
	tests := []struct {
		v    fmt.Stringer
		want string
	}{
		{AgentModeStandard, "Standard"},
		{AgentModePlan, "Plan"},
		{AgentModeAutoAccept, "AutoAccept"},
		{AgentModeReadOnly, "ReadOnly"},
		{AgentMode(99), "Unknown"},
		{ChatStatusIdle, "Idle"},
		{ChatStatusStarting, "Starting"},
		{ChatStatusThinking, "Thinking"},
		{ChatStatusGenerating, "Generating"},
		{ChatStatusReceivingTools, "ReceivingTools"},
		{ChatStatusWaitingTools, "WaitingTools"},
		{ChatStatusCompleted, "Completed"},
		{ChatStatusError, "Error"},
		{ChatStatusCancelled, "Cancelled"},
		{ChatStatus(99), "Unknown"},
		{ToolCallStatusPending, "Pending"},
		{ToolCallStatusWaitingApproval, "WaitingApproval"},
		{ToolCallStatusExecuting, "Executing"},
		{ToolCallStatusCompleted, "Completed"},
		{ToolCallStatusFailed, "Failed"},
		{ToolCallStatusCancelled, "Cancelled"},
		{ToolCallStatusDenied, "Denied"},
		{ToolCallStatus(99), "Unknown"},
		{ToolExecutionStatusIdle, "Idle"},
		{ToolExecutionStatusProcessing, "Processing"},
		{ToolExecutionStatusExecuting, "Executing"},
		{ToolExecutionStatusCompleted, "Completed"},
		{ToolExecutionStatusFailed, "Failed"},
		{ToolExecutionStatus(99), "Unknown"},
		{ApprovalApprove, "Approve"},
		{ApprovalReject, "Reject"},
		{ApprovalAutoAccept, "Auto-Accept"},
		{ApprovalAction(99), "Unknown"},
		{PlanApprovalAccept, "Accept"},
		{PlanApprovalReject, "Reject"},
		{PlanApprovalAcceptStandard, "Approve Each Step"},
		{PlanApprovalAction(99), "Unknown"},
		{AgentStateUnknown, "Unknown"},
		{AgentStatePullingImage, "PullingImage"},
		{AgentStateStarting, "Starting"},
		{AgentStateWaitingReady, "WaitingReady"},
		{AgentStateReady, "Ready"},
		{AgentStateFailed, "Failed"},
		{AgentState(99), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T/%s", tt.v, tt.want), func(t *testing.T) {
			if got := tt.v.String(); got != tt.want {
				t.Errorf("%T(%v).String() = %q, want %q", tt.v, tt.v, got, tt.want)
			}
		})
	}
}

func TestAgentModeDisplayName(t *testing.T) {
	tests := []struct {
		m    AgentMode
		want string
	}{
		{AgentModeStandard, "Standard"},
		{AgentModePlan, "Plan Mode"},
		{AgentModeAutoAccept, "Auto-Accept"},
		{AgentModeReadOnly, "Read-Only"},
		{AgentMode(99), "Unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.m.DisplayName(); got != tt.want {
				t.Errorf("AgentMode(%d).DisplayName() = %q, want %q", tt.m, got, tt.want)
			}
		})
	}
}

func TestAgentStateDisplayName(t *testing.T) {
	tests := []struct {
		s    AgentState
		want string
	}{
		{AgentStateUnknown, "unknown"},
		{AgentStatePullingImage, "pulling image"},
		{AgentStateStarting, "starting"},
		{AgentStateWaitingReady, "waiting"},
		{AgentStateReady, "ready"},
		{AgentStateFailed, "failed"},
		{AgentState(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.s.DisplayName(); got != tt.want {
				t.Errorf("AgentState(%d).DisplayName() = %q, want %q", tt.s, got, tt.want)
			}
		})
	}
}

func TestSkillDisplayNameAndSummary(t *testing.T) {
	tests := []struct {
		name  string
		skill Skill
		want  string
	}{
		{"plain", Skill{Name: "tmux", Scope: SkillScopeProject}, "tmux"},
		{"plugin qualified", Skill{Name: "review", Scope: SkillScopePlugin, PluginName: "ponytail"}, "ponytail:review"},
		{"plugin scope without plugin name", Skill{Name: "review", Scope: SkillScopePlugin}, "review"},
		{"plugin name without plugin scope", Skill{Name: "review", Scope: SkillScopeUser, PluginName: "ponytail"}, "review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.skill.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %q, want %q", got, tt.want)
			}
			sum := tt.skill.Summary()
			if sum.Name != tt.want || sum.Scope != string(tt.skill.Scope) || sum.Description != tt.skill.Description {
				t.Errorf("Summary() = %+v, want name %q scope %q", sum, tt.want, tt.skill.Scope)
			}
		})
	}
}
