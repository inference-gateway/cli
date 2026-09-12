package ipc

import (
	"encoding/json"
)

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
	Scope      string `json:"scope,omitempty"`
}

// UserQuestionRequest is emitted on stdout when the AskUserQuestion tool asks
// the user to pick from a small form. Questions carries the tool's own
// []agentdomain.UserQuestion, kept as raw JSON so ipc stays dependency-free.
type UserQuestionRequest struct {
	Type       string          `json:"type"` // "user_question_request"
	ToolCallID string          `json:"tool_call_id"`
	Questions  json.RawMessage `json:"questions"`
}

// UserQuestionResponse is written to the agent's stdin by the host UI after
// the user submits or dismisses the form. Answers is the host's
// []agentdomain.UserQuestionAnswer; Cancelled reports a dismissal.
type UserQuestionResponse struct {
	Type       string          `json:"type"` // "user_question_response"
	ToolCallID string          `json:"tool_call_id"`
	Answers    json.RawMessage `json:"answers,omitempty"`
	Cancelled  bool            `json:"cancelled,omitempty"`
}

// ComputerUseControlMessage is written to the agent's stdin by a host UI to
// pause or resume computer-use execution. Follows the same IPC pattern as
// ApprovalResponse.
type ComputerUseControlMessage struct {
	Type   string `json:"type"`   // "computer_use_control"
	Action string `json:"action"` // "pause" or "resume"
}

// AgentErrorMessage is emitted by the agent on stdout when a fatal error occurs
// before exiting. The channel manager forwards this to the user-facing channel
// so users aren't left waiting in silence when the agent process fails.
type AgentErrorMessage struct {
	Type    string `json:"type"` // "agent_error"
	Message string `json:"message"`
}
