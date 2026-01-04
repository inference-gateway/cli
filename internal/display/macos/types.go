//go:build darwin

package macos

import (
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ApprovalResponse represents a response from the Swift window
// when the user approves or rejects a tool execution
type ApprovalResponse struct {
	CallID string                `json:"call_id"`
	Action domain.ApprovalAction `json:"action"`
}

// WindowEvent wraps domain.ChatEvent with additional metadata for the window
type WindowEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

// WindowState represents the current state snapshot of the floating window
// Used for reconnection and state synchronization
type WindowState struct {
	SessionID        string            `json:"session_id"`
	IsActive         bool              `json:"is_active"`
	CurrentStatus    string            `json:"current_status"`
	PendingApprovals []PendingApproval `json:"pending_approvals"`
	ActivityCount    int               `json:"activity_count"`
}

// PendingApproval represents a tool waiting for user approval
type PendingApproval struct {
	CallID    string         `json:"call_id"`
	ToolName  string         `json:"tool_name"`
	Arguments map[string]any `json:"arguments"`
	Timestamp int64          `json:"timestamp"`
}
