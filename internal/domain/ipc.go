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

// AgentErrorMessage is emitted by the agent on stdout when a fatal error occurs
// before exiting. The channel manager forwards this to the user-facing channel
// so users aren't left waiting in silence when the agent process fails.
type AgentErrorMessage struct {
	Type    string `json:"type"` // "agent_error"
	Message string `json:"message"`
}
