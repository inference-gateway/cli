package ipc

import (
	"encoding/json"
)

// ApprovalRequest is the "approval_request" line the agent emits on stdout when
// a tool needs user approval. The channel manager prompts the user and writes
// an ApprovalResponse to stdin.
type ApprovalRequest struct {
	Type       string `json:"type"`
	ToolName   string `json:"tool_name"`
	ToolArgs   string `json:"tool_args"`
	ToolCallID string `json:"tool_call_id"`
}

// ApprovalResponse is the "approval_response" the channel manager writes to the
// agent's stdin after the user decides.
type ApprovalResponse struct {
	Type       string `json:"type"`
	ToolCallID string `json:"tool_call_id"`
	Approved   bool   `json:"approved"`
	Scope      string `json:"scope,omitempty"`
}

// UserQuestionRequest is the "user_question_request" line emitted on stdout when
// AskUserQuestion asks the user to pick from a small form. Questions carries the
// tool's []agentdomain.UserQuestion as raw JSON so ipc stays dependency-free.
type UserQuestionRequest struct {
	Type       string          `json:"type"`
	ToolCallID string          `json:"tool_call_id"`
	Questions  json.RawMessage `json:"questions"`
}

// UserQuestionResponse is the "user_question_response" the host UI writes to
// the agent's stdin after the user submits or dismisses the form. Answers is
// the []agentdomain.UserQuestionAnswer, and Cancelled reports a dismissal.
type UserQuestionResponse struct {
	Type       string          `json:"type"`
	ToolCallID string          `json:"tool_call_id"`
	Answers    json.RawMessage `json:"answers,omitempty"`
	Cancelled  bool            `json:"cancelled,omitempty"`
}

// PlanApprovalResponse is the "plan_approval_response" the host UI writes to
// the agent's stdin after the user decides on a plan. Action is one of
// PlanAccept, PlanAcceptStandard or PlanReject.
type PlanApprovalResponse struct {
	Type   string `json:"type"`
	PlanID string `json:"plan_id"`
	Action string `json:"action"`
}

// The plan decisions a PlanApprovalResponse or a plan interrupt's resume
// payload carries: execute in auto-accept mode, execute approving each step,
// or keep planning.
const (
	PlanAccept         = "accept"
	PlanAcceptStandard = "accept_standard"
	PlanReject         = "reject"
)

// RunAgentInputFrameType is the stdin frame that starts or continues a run
// with new user messages. Its body is an AG-UI RunAgentInput.
const RunAgentInputFrameType = "run_agent_input"

// UserMessageRequestID tags queue entries that came from a run input on stdin
// so the renderer can tell them apart from background-job result notes.
const UserMessageRequestID = "stdin"

// AgentErrorMessage is the "agent_error" line the agent emits on stdout when a
// fatal error occurs before exiting. The channel manager forwards it so users
// are not left waiting in silence.
type AgentErrorMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}
