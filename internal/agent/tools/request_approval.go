package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// RequestApprovalTool lets the agent ask the user to override a judge-rejected
// tool call (issue #1156): it presents the rejected call, the judge's reason
// and the agent's justification through the regular tool approval box, and
// returns the user's decision. An approval sets a one-shot bypass so the next
// matching judge decision approves without a judge call.
//
// The escalation gate is injected into the execution context only on the chat
// path (where the approval box can reach the user); headless/no-TTY runs see
// a nil gate and degrade with a distinguishable "no approver reachable" result
// instead of blocking, mirroring the AskUserQuestion degrade.
type RequestApprovalTool struct {
	config    *config.Config
	enabled   bool
	formatter agentinfra.BaseFormatter
}

// NewRequestApprovalTool creates a new RequestApproval tool.
func NewRequestApprovalTool(cfg *config.Config) *RequestApprovalTool {
	return &RequestApprovalTool{
		config:    cfg,
		enabled:   true,
		formatter: agentinfra.NewBaseFormatter("RequestApproval"),
	}
}

// Definition returns the tool definition for the LLM.
func (t *RequestApprovalTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.RequestApproval.Description

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "RequestApproval",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"additionalProperties": false,
				"type":                 "object",
				"required":             []string{"tool", "arguments", "what", "why"},
				"properties": map[string]any{
					"tool": map[string]any{
						"type":        "string",
						"description": "Name of the judge-rejected tool you want to run.",
					},
					"arguments": map[string]any{
						"type":        "object",
						"description": "The exact arguments of the rejected call (must match the rejected invocation).",
					},
					"what": map[string]any{
						"type":        "string",
						"description": "What you need permission for, in one sentence.",
					},
					"why": map[string]any{
						"type":        "string",
						"description": "Why the action serves the user's request, in your own words.",
					},
				},
			},
		},
	}
}

// Execute escalates one judge-rejected call to the user and returns the
// decision. It blocks (in its own goroutine) until the user answers, dismisses,
// or the session is cancelled.
func (t *RequestApprovalTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()

	req, err := extractEscalationRequest(args)
	if err != nil {
		return t.failure(args, start, err.Error()), nil
	}

	gate := agentdomain.GetApprovalEscalation(ctx)
	if gate == nil {
		logger.Debug("RequestApproval: no interactive approver in context - degrading")
		return t.result(args, start, agentdomain.EscalationNoApprover, false, map[string]any{
			"available": false,
			"message": "No interactive approver is reachable in this session (headless/no-TTY run), so the " +
				"escalation cannot be asked. STOP: do not call RequestApproval again; either proceed without " +
				"running the rejected call or end your turn and let the user intervene.",
		}), nil
	}

	outcome, err := gate.Escalate(ctx, req)
	if err != nil {
		return t.failure(args, start, err.Error()), nil
	}

	data := map[string]any{"judge_reason": outcome.JudgeReason}

	switch outcome.Status {
	case agentdomain.EscalationApproved:
		data["message"] = fmt.Sprintf("The user APPROVED running %s. Re-issue the exact same tool call (same name and arguments) now; it runs without another approval prompt - the judge is bypassed for that one invocation.", req.ToolCall.Function.Name)
		return t.result(args, start, outcome.Status, true, data), nil

	case agentdomain.EscalationDenied:
		data["message"] = fmt.Sprintf("The user DENIED running %s. Adjust your next step accordingly (the user may explain in their next message); do not call RequestApproval for this call again.", req.ToolCall.Function.Name)
		return t.result(args, start, outcome.Status, false, data), nil

	case agentdomain.EscalationNotRejected:
		return t.failure(args, start, "RequestApproval only escalates calls the judge rejected; no judge rejection is pending for "+req.ToolCall.Function.Name+" with these arguments. Make the call first - the judge decides it - and escalate only if the result says it was rejected by the judge."), nil

	case agentdomain.EscalationAlreadyAsked:
		return t.failure(args, start, "This rejected call was already escalated once; the user's decision stands. Do not ask again."), nil

	default:
		return t.failure(args, start, fmt.Sprintf("unexpected escalation status %q", outcome.Status)), nil
	}
}

// Validate checks that the escalation arguments satisfy the schema.
func (t *RequestApprovalTool) Validate(args map[string]any) error {
	_, err := extractEscalationRequest(args)
	return err
}

// IsEnabled is always true: every mode advertises the same tool list so a
// mode switch never invalidates the provider prompt cache. Outside judge mode
// no rejection is tracked, so Execute answers "not_rejected".
func (t *RequestApprovalTool) IsEnabled() bool {
	return t.enabled
}

// extractEscalationRequest parses and validates the tool arguments into the
// domain request. Shared by Validate and Execute so both apply identical rules.
func extractEscalationRequest(args map[string]any) (agentdomain.ApprovalEscalationRequest, error) {
	toolName, ok := args["tool"].(string)
	if !ok || strings.TrimSpace(toolName) == "" {
		return agentdomain.ApprovalEscalationRequest{}, fmt.Errorf("tool parameter is required and must be a non-empty string")
	}

	rawArgs, present := args["arguments"]
	if !present {
		return agentdomain.ApprovalEscalationRequest{}, fmt.Errorf("arguments parameter is required (use {} for a call without arguments)")
	}
	argsJSON, err := json.Marshal(rawArgs)
	if err != nil {
		return agentdomain.ApprovalEscalationRequest{}, fmt.Errorf("arguments must be a JSON object: %w", err)
	}

	what, ok := args["what"].(string)
	if !ok || strings.TrimSpace(what) == "" {
		return agentdomain.ApprovalEscalationRequest{}, fmt.Errorf("what parameter is required and must be a non-empty string")
	}
	why, ok := args["why"].(string)
	if !ok || strings.TrimSpace(why) == "" {
		return agentdomain.ApprovalEscalationRequest{}, fmt.Errorf("why parameter is required and must be a non-empty string")
	}

	return agentdomain.ApprovalEscalationRequest{
		ToolCall: sdk.ChatCompletionMessageToolCall{
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      strings.TrimSpace(toolName),
				Arguments: string(argsJSON),
			},
		},
		What: strings.TrimSpace(what),
		Why:  strings.TrimSpace(why),
	}, nil
}

func (t *RequestApprovalTool) result(args map[string]any, start time.Time, status string, approved bool, data map[string]any) *agentdomain.ToolExecutionResult {
	data["status"] = status
	data["approved"] = approved
	return &agentdomain.ToolExecutionResult{
		ToolName:  "RequestApproval",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      data,
	}
}

func (t *RequestApprovalTool) failure(args map[string]any, start time.Time, msg string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "RequestApproval",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     msg,
	}
}

// FormatResult formats tool execution results for different contexts.
func (t *RequestApprovalTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *RequestApprovalTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return "Escalation not possible"
	}
	switch resultStatus(result) {
	case agentdomain.EscalationApproved:
		return "Approved by user"
	case agentdomain.EscalationDenied:
		return "Denied by user"
	case agentdomain.EscalationNoApprover:
		return "No approver reachable"
	default:
		return "Escalated to user"
	}
}

// FormatForUI formats the result for UI display.
func (t *RequestApprovalTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	return fmt.Sprintf("RequestApproval(...)\n└─ %s %s", statusIcon, t.FormatPreview(result))
}

// FormatForLLM formats the result for LLM consumption.
func (t *RequestApprovalTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return fmt.Sprintf("Failed to escalate to the user: %s", result.Error)
	}
	if msg := escalationResultMessage(result); msg != "" {
		return msg
	}
	return "The user answered the escalation."
}

// ShouldCollapseArg determines if an argument should be collapsed in display.
func (t *RequestApprovalTool) ShouldCollapseArg(key string) bool {
	return key == "arguments"
}

// ShouldAlwaysExpand keeps the escalation outcome expanded in the conversation
// by default - the decision (and any user answer) is the whole point of the tool.
func (t *RequestApprovalTool) ShouldAlwaysExpand() bool {
	return true
}

// resultStatus reads the escalation status stored in the result Data map.
func resultStatus(result *agentdomain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	status, _ := data["status"].(string)
	return status
}

func escalationResultMessage(result *agentdomain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	msg, _ := data["message"].(string)
	return msg
}
