// Package render formats domain.ChatEvent streams for the headless CLI.
//
// Each format function consumes the event channel from
// AgentService.RunWithStream and writes formatted output to an io.Writer.
// All renderers drain the channel until it closes (the engine closes it when
// the run ends), so the producer is never left blocked mid-run.
package render

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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
func assistantMessage(e domain.ChatCompleteEvent, content string) map[string]any {
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
func toolMessage(r *domain.ToolExecutionResult) map[string]any {
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
func toolContent(r *domain.ToolExecutionResult) string {
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
func completionErr(e domain.ChatCompleteEvent) error {
	if e.Cancelled {
		return context.Canceled
	}
	if e.MaxTurnsReached {
		return domain.ErrMaxTurnsReached
	}
	return nil
}

// answerApproval reads approval_response lines from in and answers the
// engine's pending approval on the event's response channel. Responses
// carrying a different tool_call_id are skipped (a late answer to a request
// the engine already timed out must not decide the next one). Any read or
// parse failure rejects the tool so the engine never waits out its timeout
// on a dead broker.
// When controlMessages is non-nil, computer_use_control lines are forwarded
// instead of being treated as invalid approval responses.
func answerApproval(e domain.ToolApprovalRequestedEvent, in *bufio.Scanner, controlMessages chan<- domain.ComputerUseControlMessage) {
	if e.ResponseChan == nil {
		return
	}
	for in != nil && in.Scan() {
		var resp domain.ApprovalResponse
		if err := json.Unmarshal(in.Bytes(), &resp); err != nil || resp.Type != "approval_response" {
			// Check for a control message to forward
			var ctrl domain.ComputerUseControlMessage
			if controlMessages != nil && json.Unmarshal(in.Bytes(), &ctrl) == nil && ctrl.Type == "computer_use_control" {
				controlMessages <- ctrl
			}
			continue
		}
		if resp.ToolCallID != "" && resp.ToolCallID != e.ToolCall.ID {
			continue
		}
		if resp.Approved {
			e.ResponseChan <- domain.ApprovalApprove
		} else {
			e.ResponseChan <- domain.ApprovalReject
		}
		return
	}
	e.ResponseChan <- domain.ApprovalReject
}

// RenderJSON renders events as JSON lines, streaming each message as its turn
// completes: assistant messages on ChatCompleteEvent, tool results on
// ToolExecutionCompletedEvent, approval requests and errors in real time.
// After the channel closes only the session stats are read from the repo.
// When in is non-nil it acts as the IPC approval broker: each approval_request
// line is answered by reading an approval_response line from in.
func RenderJSON(events <-chan domain.ChatEvent, w io.Writer, in io.Reader, sessionID, model string, cfg *config.Config, repo domain.ConversationRepository) error {
	return renderJSON(events, w, in, sessionID, model, cfg, repo, false)
}

// RenderJSONPretty is RenderJSON with each object indented across multiple
// lines for human reading. Objects are separated by newlines but are no
// longer one-per-line, so machine consumers should use RenderJSON.
func RenderJSONPretty(events <-chan domain.ChatEvent, w io.Writer, in io.Reader, sessionID, model string, cfg *config.Config, repo domain.ConversationRepository) error {
	return renderJSON(events, w, in, sessionID, model, cfg, repo, true)
}

func renderJSON(events <-chan domain.ChatEvent, w io.Writer, in io.Reader, sessionID, model string, cfg *config.Config, repo domain.ConversationRepository, pretty bool) error {
	emit := func(msg any) { emitJSON(w, msg, pretty) }
	emit(map[string]any{
		"type":       "info",
		"message":    "Starting new agent session",
		"session_id": sessionID,
		"model":      model,
		"timestamp":  time.Now(),
	})

	var stdin *bufio.Scanner
	if in != nil {
		stdin = bufio.NewScanner(in)
	}

	var runErr error
	var content strings.Builder

	var controlMessages chan domain.ComputerUseControlMessage
	if stdin != nil {
		controlMessages = make(chan domain.ComputerUseControlMessage, 4)
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				// Channel closed - exit the loop naturally
				goto done
			}
			switch e := event.(type) {
			case domain.ChatErrorEvent:
				emit(domain.AgentErrorMessage{Type: "agent_error", Message: truncate(e.Error.Error(), 3500)})
				runErr = fmt.Errorf("agent error: %w", e.Error)
			case domain.ChatChunkEvent:
				content.WriteString(e.Content)
			case domain.ChatCompleteEvent:
				if msg := assistantMessage(e, content.String()); msg != nil {
					emit(msg)
				}
				content.Reset()
				if err := completionErr(e); err != nil {
					runErr = err
				}
			case domain.ToolExecutionCompletedEvent:
				for _, r := range e.Results {
					if r != nil {
						emit(toolMessage(r))
					}
				}
			case domain.ToolApprovalRequestedEvent:
				emit(map[string]any{
					"type": "approval_request", "tool_name": e.ToolCall.Function.Name,
					"tool_args": e.ToolCall.Function.Arguments, "tool_call_id": e.ToolCall.ID,
				})
				answerApproval(e, stdin, controlMessages)
			case domain.ComputerUsePausedEvent:
				emit(map[string]any{"type": "computer_use_paused", "request_id": e.RequestID})
			case domain.ComputerUseResumedEvent:
				emit(map[string]any{"type": "computer_use_resumed", "request_id": e.RequestID})
			case domain.TodoUpdateChatEvent:
				emit(map[string]any{"type": "notification", "message": "Todos updated", "todos": e.Todos})
			}
		case ctrl := <-controlMessages:
			emit(map[string]any{
				"type": "computer_use_" + ctrl.Action, "request_id": "",
			})
		}
	}
done:

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
func RenderText(events <-chan domain.ChatEvent, w io.Writer) error {
	var runErr error
	printed := false
	for event := range events {
		switch e := event.(type) {
		case domain.ChatChunkEvent:
			if e.Content != "" {
				_, _ = fmt.Fprint(w, e.Content)
				printed = true
			}
		case domain.ChatCompleteEvent:
			if printed {
				_, _ = fmt.Fprintln(w)
				printed = false
			}
			if err := completionErr(e); err != nil {
				runErr = err
			}
		case domain.ChatErrorEvent:
			runErr = fmt.Errorf("agent error: %w", e.Error)
		}
	}
	return runErr
}

// RenderAGUI renders events as newline-delimited AG-UI protocol events. The
// run gets exactly one RUN_STARTED and one terminal event (RUN_FINISHED or
// RUN_ERROR); per-turn events in between carry deltas, tool calls, and results.
// When in is non-nil it acts as the IPC approval broker, same as RenderJSON.
func RenderAGUI(events <-chan domain.ChatEvent, w io.Writer, in io.Reader, sessionID, model string) error {
	e := &aguiEncoder{w: w, threadID: sessionID}
	e.emitRunStarted(sessionID)

	var stdin *bufio.Scanner
	if in != nil {
		stdin = bufio.NewScanner(in)
	}

	var runErr error
	for event := range events {
		switch ev := event.(type) {
		case domain.ChatChunkEvent:
			if ev.ReasoningContent != "" {
				e.streamReasoning(ev.ReasoningContent)
			}
			if ev.Content != "" {
				e.streamText(ev.Content)
			}
		case domain.ChatCompleteEvent:
			e.closeMessage()
			for _, tc := range ev.ToolCalls {
				e.emitToolCallStart(tc.ID, tc.Function.Name)
				e.emitToolCallArgs(tc.ID, tc.Function.Arguments)
				e.emitToolCallEnd(tc.ID)
			}
			if err := completionErr(ev); err != nil {
				runErr = err
			}
		case domain.ChatErrorEvent:
			runErr = ev.Error
		case domain.ToolExecutionCompletedEvent:
			for _, r := range ev.Results {
				if r != nil {
					e.emitToolResult(r)
				}
			}
		case domain.TodoUpdateChatEvent:
			e.emitTodos(ev.Todos)
		case domain.ToolApprovalRequestedEvent:
			e.emitApprovalRequest(domain.ApprovalRequest{
				Type: "approval_request", ToolName: ev.ToolCall.Function.Name,
				ToolArgs: ev.ToolCall.Function.Arguments, ToolCallID: ev.ToolCall.ID,
			})
			answerApproval(ev, stdin, nil)
		case domain.ComputerUsePausedEvent:
			e.emitComputerUsePaused(ev.RequestID)
		case domain.ComputerUseResumedEvent:
			e.emitComputerUseResumed(ev.RequestID)
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
		emitJSON(w, domain.AgentErrorMessage{Type: "agent_error", Message: truncate(err.Error(), 3500)}, format == "json-pretty")
	case "ag-ui":
		(&aguiEncoder{w: w}).emitRunError(truncate(err.Error(), 3500))
	}
}
