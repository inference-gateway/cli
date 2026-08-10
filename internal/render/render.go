// Package render formats domain.ChatEvent streams for the headless CLI.
//
// Each format function consumes the event channel from
// AgentService.RunWithStream and writes formatted output to an io.Writer.
// All renderers drain the channel until it closes (the engine closes it when
// the run ends), so the producer is never left blocked mid-run.
package render

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

func emitJSON(w io.Writer, msg any) {
	data, err := json.Marshal(msg)
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

// entryMessage builds a map from a conversation entry for JSON output.
func entryMessage(entry domain.ConversationEntry) map[string]any {
	msg := map[string]any{
		"role":      string(entry.Message.Role),
		"timestamp": entry.Time,
	}
	if content, err := entry.Message.Content.AsMessageContent0(); err == nil {
		msg["content"] = content
	}
	if entry.ReasoningContent != "" {
		msg["reasoning_content"] = entry.ReasoningContent
	}
	if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
		msg["tool_calls"] = *entry.Message.ToolCalls
	}
	if entry.Message.ToolCallID != nil {
		msg["tool_call_id"] = *entry.Message.ToolCallID
	}
	if entry.ToolExecution != nil {
		msg["failed"] = !entry.ToolExecution.Success
		msg["tool_execution"] = map[string]any{
			"tool_name": entry.ToolExecution.ToolName,
			"success":   entry.ToolExecution.Success,
			"error":     entry.ToolExecution.Error,
			"rejected":  entry.ToolExecution.Rejected,
			"duration":  entry.ToolExecution.Duration.String(),
		}
	}
	return msg
}

// completionErr maps a terminal event to the error the command should return:
// ErrMaxTurnsReached for a turn-limit completion (exit code 2), nil otherwise.
func completionErr(e domain.ChatCompleteEvent) error {
	if e.MaxTurnsReached {
		return domain.ErrMaxTurnsReached
	}
	return nil
}

// answerApproval reads approval_response lines from in and answers the
// engine's pending approval on the event's response channel. Any read or
// parse failure rejects the tool so the engine never waits out its timeout
// on a dead broker.
func answerApproval(e domain.ToolApprovalRequestedEvent, in *bufio.Scanner) {
	if e.ResponseChan == nil {
		return
	}
	for in != nil && in.Scan() {
		var resp domain.ApprovalResponse
		if err := json.Unmarshal(in.Bytes(), &resp); err != nil || resp.Type != "approval_response" {
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

// RenderJSON renders events as JSON lines. It emits session info at start,
// forwards approval requests and errors in real time, and after the event
// channel closes reads the full conversation and session stats from the repo.
// When in is non-nil it acts as the IPC approval broker: each approval_request
// line is answered by reading an approval_response line from in.
func RenderJSON(events <-chan domain.ChatEvent, w io.Writer, in io.Reader, sessionID, model string, cfg *config.Config, repo domain.ConversationRepository) error {
	emitJSON(w, map[string]any{
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
	for event := range events {
		switch e := event.(type) {
		case domain.ChatErrorEvent:
			emitJSON(w, map[string]any{"type": "agent_error", "message": truncate(e.Error.Error(), 3500)})
			runErr = fmt.Errorf("agent error: %w", e.Error)
		case domain.ChatCompleteEvent:
			if err := completionErr(e); err != nil {
				runErr = err
			}
		case domain.ToolApprovalRequestedEvent:
			emitJSON(w, map[string]any{
				"type": "approval_request", "tool_name": e.ToolCall.Function.Name,
				"tool_args": e.ToolCall.Function.Arguments, "tool_call_id": e.ToolCall.ID,
			})
			answerApproval(e, stdin)
		case domain.TodoUpdateChatEvent:
			emitJSON(w, map[string]any{"type": "notification", "message": "Todos updated", "todos": e.Todos})
		}
	}

	// Dump the full conversation from the repo.
	for _, entry := range repo.GetMessages() {
		if entry.Hidden {
			continue
		}
		emitJSON(w, entryMessage(entry))
	}

	// Session stats.
	tokenStats := repo.GetSessionTokens()
	if tokenStats.RequestCount <= 0 {
		return runErr
	}
	costStats := repo.GetSessionCostStats()
	currency := "USD"
	if cfg != nil && cfg.Pricing.Currency != "" {
		currency = cfg.Pricing.Currency
	}
	emitJSON(w, map[string]any{
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
			if printed { // tool-only turns produce no text, so no blank line
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
func RenderAGUI(events <-chan domain.ChatEvent, w io.Writer, sessionID, model string) error {
	e := &aguiEncoder{w: w, threadID: sessionID}
	e.emitRunStarted(sessionID)

	var runErr error
	for event := range events {
		switch ev := event.(type) {
		case domain.ChatChunkEvent:
			if ev.ReasoningContent != "" {
				e.emitReasoningDelta(ev.RequestID, ev.ReasoningContent)
			}
			if ev.Content != "" {
				e.emitTextDelta(ev.RequestID, ev.Content)
			}
		case domain.ChatCompleteEvent:
			for _, tc := range ev.ToolCalls {
				e.emitToolCallStart(tc.ID, tc.Function.Name)
				e.emitToolCallArgs(tc.ID, tc.Function.Arguments)
				e.emitToolCallEnd(tc.ID)
			}
			if ev.Cancelled {
				runErr = errors.New("cancelled")
			} else if err := completionErr(ev); err != nil {
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
		}
	}

	if runErr != nil {
		e.emitRunError(runErr.Error())
		return fmt.Errorf("agent error: %w", runErr)
	}
	e.emitRunFinished()
	return nil
}
