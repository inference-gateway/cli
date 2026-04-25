package domain

// ApprovalRequest is emitted by the agent on stdout when a tool needs user approval.
// The channel manager detects this JSON line, prompts the user, and writes an ApprovalResponse to stdin.
type ApprovalRequest struct {
	Type       string `json:"type"` // "approval_request"
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	ToolCallID string `json:"tool_call_id"`
}

// ApprovalResponse is written to the agent's stdin by the channel manager after user decision.
type ApprovalResponse struct {
	Type       string `json:"type"` // "approval_response"
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
}
