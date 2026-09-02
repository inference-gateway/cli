// Package render formats agentdomain.ChatEvent streams for the headless CLI.
//
// Each format function consumes the event channel from
// AgentService.RunWithStream and writes formatted output to an io.Writer.
// All renderers drain the channel until it closes (the engine closes it when
// the run ends), so the producer is never left blocked mid-run.
package render

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

func emitJSON(w io.Writer, msg any, pretty bool) {
	var data []byte
	var err error
	if pretty {
		data, err = json.MarshalIndent(msg, "", "  ")
	} else {
		data, err = json.Marshal(msg)
	}
	if err != nil {
		logger.Error("render: failed to marshal JSON", "error", err)
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "…"
}

// assistantMessage builds the JSON line for a completed assistant turn, or
// nil for completions with nothing to say (cancellation, max-turns). content
// is the turn's text accumulated from chunk deltas — the engine publishes
// completion events without the message body.
func assistantMessage(e agentdomain.ChatCompleteEvent, content string) map[string]any {
	if content == "" && e.ReasoningContent == "" && len(e.ToolCalls) == 0 {
		return nil
	}
	msg := map[string]any{
		"role":      "assistant",
		"content":   content,
		"timestamp": e.Timestamp,
	}
	if e.ReasoningContent != "" {
		msg["reasoning_content"] = e.ReasoningContent
	}
	if len(e.ToolCalls) > 0 {
		msg["tool_calls"] = e.ToolCalls
	}
	if e.Metrics != nil && e.Metrics.Usage != nil {
		msg["token_usage"] = e.Metrics.Usage
	}
	return msg
}

// toolMessage builds the JSON line for one tool execution result.
func toolMessage(r *agentdomain.ToolExecutionResult) map[string]any {
	return map[string]any{
		"role":         "tool",
		"content":      toolContent(r),
		"failed":       !r.Success,
		"tool_call_id": r.ToolCallID,
		"timestamp":    time.Now(),
		"tool_execution": map[string]any{
			"tool_name": r.ToolName,
			"success":   r.Success,
			"error":     r.Error,
			"rejected":  r.Rejected,
			"duration":  r.Duration.String(),
		},
	}
}

// toolContent is the marshaled result on success, the error detail on failure.
func toolContent(r *agentdomain.ToolExecutionResult) string {
	if r.Success {
		if b, err := json.Marshal(r); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", r.Data)
	}
	detail := r.Error
	if detail == "" && r.Data != nil {
		if b, err := json.Marshal(r.Data); err == nil {
			detail = string(b)
		}
	}
	return detail
}

// completionErr maps a terminal event to the error the command should return:
// ErrMaxTurnsReached for a turn-limit completion (exit code 2), nil otherwise.
func completionErr(e agentdomain.ChatCompleteEvent) error {
	if e.Cancelled {
		return context.Canceled
	}
	if e.MaxTurnsReached {
		return agentdomain.ErrMaxTurnsReached
	}
	return nil
}

// answerApproval answers the engine's pending approval on the event's
// response channel from the broker's approvals channel. Responses carrying a
// different tool_call_id are skipped (a late answer to a request the engine
// already timed out must not decide the next one). A nil or closed channel
// rejects the tool so the engine never waits out its timeout on a dead broker.
func answerApproval(e agentdomain.ToolApprovalRequestedEvent, approvals <-chan ipc.ApprovalResponse) {
	if e.ResponseChan == nil {
		return
	}
	if approvals == nil {
		e.ResponseChan <- agentdomain.ApprovalReject
		return
	}
	for resp := range approvals {
		if resp.ToolCallID != "" && resp.ToolCallID != e.ToolCall.ID {
			continue
		}
		switch {
		case resp.Approved && resp.Scope == "always":
			e.ResponseChan <- agentdomain.ApprovalAutoAccept
		case resp.Approved:
			e.ResponseChan <- agentdomain.ApprovalApprove
		default:
			e.ResponseChan <- agentdomain.ApprovalReject
		}
		return
	}
	e.ResponseChan <- agentdomain.ApprovalReject
}

// judgeStderr echoes a judge rejection to stderr in every headless format: the
// TUI flash equivalent for unattended runs. CI consumers watching stdout keep
// the machine-readable judge_verdict line.
func judgeStderr(e agentdomain.JudgeVerdictChatEvent) {
	if e.Decision != agentdomain.JudgeDecisionRejected {
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "Action rejected by judge policy: %s\n", e.Reason)
}

// judgeVerdictMessage builds the JSON line for one judge decision.
func judgeVerdictMessage(e agentdomain.JudgeVerdictChatEvent) map[string]any {
	return map[string]any{
		"type":      "judge_verdict",
		"tool":      e.Tool,
		"decision":  e.Decision,
		"reason":    e.Reason,
		"turn":      e.Turn,
		"timestamp": e.Timestamp,
	}
}

// RenderJSON renders events as JSON lines, streaming each message as its turn
// completes: assistant messages on ChatCompleteEvent, tool results on
// ToolExecutionCompletedEvent, approval requests and errors in real time.
// After the channel closes only the session stats are read from the repo.
// When approvals is non-nil it acts as the IPC approval broker: each
// approval_request line is answered by the next matching ApprovalResponse.
// A ComputerUseResumedEvent clears any error carried over from the paused
// (cancelled) run, so a resumed run that completes cleanly exits zero.
func RenderJSON(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, sessionID, model string, cfg *config.Config, repo convdomain.ConversationRepository) error {
	return renderJSON(events, w, approvals, sessionID, model, cfg, repo, false)
}

// RenderJSONPretty is RenderJSON with each object indented across multiple
// lines for human reading. Objects are separated by newlines but are no
// longer one-per-line, so machine consumers should use RenderJSON.
func RenderJSONPretty(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, sessionID, model string, cfg *config.Config, repo convdomain.ConversationRepository) error {
	return renderJSON(events, w, approvals, sessionID, model, cfg, repo, true)
}

func renderJSON(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, sessionID, model string, cfg *config.Config, repo convdomain.ConversationRepository, pretty bool) error {
	emit := func(msg any) { emitJSON(w, msg, pretty) }
	emit(map[string]any{
		"type":       "info",
		"message":    "Starting new agent session",
		"session_id": sessionID,
		"model":      model,
		"timestamp":  time.Now(),
	})

	var runErr error
	var content strings.Builder
	for event := range events {
		switch e := event.(type) {
		case agentdomain.ChatErrorEvent:
			emit(ipc.AgentErrorMessage{Type: "agent_error", Message: truncate(e.Error.Error(), 3500)})
			runErr = fmt.Errorf("agent error: %w", e.Error)
		case agentdomain.ChatChunkEvent:
			content.WriteString(e.Content)
		case agentdomain.ChatCompleteEvent:
			if msg := assistantMessage(e, content.String()); msg != nil {
				emit(msg)
			}
			content.Reset()
			if err := completionErr(e); err != nil {
				runErr = err
			}
		case agentdomain.ToolExecutionCompletedEvent:
			for _, r := range e.Results {
				if r != nil {
					emit(toolMessage(r))
				}
			}
		case agentdomain.ToolApprovalRequestedEvent:
			emit(map[string]any{
				"type": "approval_request", "tool_name": e.ToolCall.Function.Name,
				"tool_args": e.ToolCall.Function.Arguments, "tool_call_id": e.ToolCall.ID,
			})
			answerApproval(e, approvals)
		case agentdomain.ComputerUsePausedEvent:
			emit(map[string]any{"type": "computer_use_paused", "request_id": e.RequestID})
		case agentdomain.ComputerUseResumedEvent:
			emit(map[string]any{"type": "computer_use_resumed", "request_id": e.RequestID})
			runErr = nil
		case agentdomain.TodoUpdateChatEvent:
			emit(map[string]any{"type": "notification", "message": "Todos updated", "todos": e.Todos})
		case agentdomain.JudgeVerdictChatEvent:
			emit(judgeVerdictMessage(e))
			judgeStderr(e)
		}
	}

	tokenStats := repo.GetSessionTokens()
	if tokenStats.RequestCount <= 0 {
		return runErr
	}
	costStats := repo.GetSessionCostStats()
	currency := "USD"
	if cfg != nil && cfg.Pricing.Currency != "" {
		currency = cfg.Pricing.Currency
	}
	emit(map[string]any{
		"type": "session_stats", "message": "Session complete", "model": model,
		"prompt_tokens": tokenStats.TotalInputTokens, "completion_tokens": tokenStats.TotalOutputTokens,
		"total_tokens": tokenStats.TotalTokens, "requests": tokenStats.RequestCount,
		"cost": map[string]any{"input": costStats.TotalInputCost, "output": costStats.TotalOutputCost, "total": costStats.TotalCost, "currency": currency},
	})
	return runErr
}

// RenderText streams content deltas as plain text to w, matching the
// non-interactive chat output style. The engine emits one ChatCompleteEvent
// per turn, so rendering continues until the channel closes.
func RenderText(events <-chan agentdomain.ChatEvent, w io.Writer) error {
	var runErr error
	printed := false
	for event := range events {
		switch e := event.(type) {
		case agentdomain.ChatChunkEvent:
			if e.Content != "" {
				_, _ = fmt.Fprint(w, e.Content)
				printed = true
			}
		case agentdomain.ChatCompleteEvent:
			if printed {
				_, _ = fmt.Fprintln(w)
				printed = false
			}
			if err := completionErr(e); err != nil {
				runErr = err
			}
		case agentdomain.ChatErrorEvent:
			runErr = fmt.Errorf("agent error: %w", e.Error)
		case agentdomain.JudgeVerdictChatEvent:
			judgeStderr(e)
		}
	}
	return runErr
}

// RenderAGUI renders events as newline-delimited AG-UI protocol events. The
// run gets exactly one RUN_STARTED and one terminal event (RUN_FINISHED or
// RUN_ERROR); per-turn events in between carry deltas, tool calls, and results.
// When approvals is non-nil it acts as the IPC approval broker, same as
// RenderJSON. A ComputerUseResumedEvent clears any error carried over from
// the paused (cancelled) run, same as RenderJSON.
func RenderAGUI(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, sessionID, model string) error {
	e := &aguiEncoder{w: w, threadID: sessionID}
	e.emitRunStarted(sessionID)

	var runErr error
	for event := range events {
		switch ev := event.(type) {
		case agentdomain.ChatChunkEvent:
			if ev.ReasoningContent != "" {
				e.streamReasoning(ev.ReasoningContent)
			}
			if ev.Content != "" {
				e.streamText(ev.Content)
			}
		case agentdomain.ChatCompleteEvent:
			e.closeMessage()
			for _, tc := range ev.ToolCalls {
				e.emitToolCallStart(tc.ID, tc.Function.Name)
				e.emitToolCallArgs(tc.ID, tc.Function.Arguments)
				e.emitToolCallEnd(tc.ID)
			}
			if err := completionErr(ev); err != nil {
				runErr = err
			}
		case agentdomain.ChatErrorEvent:
			runErr = ev.Error
		case agentdomain.UserMessageChatEvent:
			e.emitUserMessage(ev.Content)
		case agentdomain.ToolExecutionCompletedEvent:
			for _, r := range ev.Results {
				if r != nil {
					e.emitToolResult(r)
				}
			}
		case agentdomain.TodoUpdateChatEvent:
			e.emitTodos(ev.Todos)
		case agentdomain.JudgeVerdictChatEvent:
			e.emitJudgeVerdict(ev)
			judgeStderr(ev)
		case agentdomain.ToolApprovalRequestedEvent:
			e.emitApprovalRequest(ipc.ApprovalRequest{
				Type: "approval_request", ToolName: ev.ToolCall.Function.Name,
				ToolArgs: ev.ToolCall.Function.Arguments, ToolCallID: ev.ToolCall.ID,
			})
			answerApproval(ev, approvals)
		case agentdomain.ComputerUsePausedEvent:
			e.emitComputerUsePaused(ev.RequestID)
		case agentdomain.ComputerUseResumedEvent:
			e.emitComputerUseResumed(ev.RequestID)
			runErr = nil
		}
	}

	e.closeMessage()
	if runErr != nil {
		e.emitRunError(runErr.Error())
		return fmt.Errorf("agent error: %w", runErr)
	}
	e.emitRunFinished()
	return nil
}

// EmitPreRunError writes a machine-readable failure line for errors that occur
// before the event stream starts (gateway down, unknown model, ...), so stdout
// consumers - the channel manager and agentrunner - see the failure instead of
// silence. The text format stays quiet; the CLI's stderr prose covers it.
func EmitPreRunError(w io.Writer, format string, err error) {
	switch format {
	case "json", "json-pretty":
		emitJSON(w, ipc.AgentErrorMessage{Type: "agent_error", Message: truncate(err.Error(), 3500)}, format == "json-pretty")
	case "ag-ui":
		(&aguiEncoder{w: w}).emitRunError(truncate(err.Error(), 3500))
	}
}
