package cmd

import (
	"bytes"
	"strings"
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// TestAguiEncoderFullRun records a complete headless run through the encoder
// and asserts the emitted lines decode with the AG-UI SDK into a valid event
// sequence: bracketed by RUN_STARTED/RUN_FINISHED, correct start/end pairing,
// no orphan ids.
func TestAguiEncoderFullRun(t *testing.T) {
	var buf bytes.Buffer
	e := &aguiEncoder{w: &buf}

	e.emitRunStarted("session-1")
	e.emitMessagesSnapshot([]ConversationMessage{
		{Role: "user", Content: "earlier task"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "system", Content: "hidden"},
		{Role: "user", Content: "internal", Internal: true},
	})
	e.emitMessage(ConversationMessage{Role: "user", Content: "fix the bug"})
	toolCalls := []sdk.ChatCompletionMessageToolCall{{
		ID:   "call-1",
		Type: sdk.ChatCompletionToolType("function"),
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      "Bash",
			Arguments: `{"command":"ls"}`,
		},
	}}
	e.emitMessage(ConversationMessage{
		Role:      "assistant",
		Content:   "Listing files first.",
		RequestID: "req-1",
		ToolCalls: &toolCalls,
	})
	e.emitMessage(ConversationMessage{
		Role:       "tool",
		Content:    `Result of tool call: {"tool_name":"Bash"}`,
		ToolCallID: "call-1",
		ToolExecution: &domain.ToolExecutionResult{
			ToolName: "Bash",
			Success:  true,
		},
	})
	e.emitTodos([]domain.TodoItem{{Content: "fix the bug", Status: "in_progress"}})
	e.emitApprovalRequest(domain.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"rm -rf build"}`,
		ToolCallID: "call-2",
	})
	e.emitMessage(ConversationMessage{Role: "assistant", Content: "Done.", RequestID: "req-2"})
	e.result = map[string]any{"total_tokens": 42}
	e.emitRunFinished()

	events := decodeLines(t, buf.String())

	if err := aguievents.ValidateSequence(events); err != nil {
		t.Fatalf("event sequence invalid: %v", err)
	}

	if events[0].Type() != aguievents.EventTypeRunStarted {
		t.Errorf("first event = %s, want RUN_STARTED", events[0].Type())
	}
	last := events[len(events)-1]
	if last.Type() != aguievents.EventTypeRunFinished {
		t.Errorf("last event = %s, want RUN_FINISHED", last.Type())
	}

	result, ok := last.(*aguievents.RunFinishedEvent)
	if !ok || result.Result == nil {
		t.Errorf("RUN_FINISHED missing session-stats result: %+v", last)
	}

	tr := findToolCallResult(t, events, "call-1")
	if strings.Contains(tr.Content, "Result of tool call") {
		t.Errorf("TOOL_CALL_RESULT content still carries the legacy prefix: %q", tr.Content)
	}
	if !strings.Contains(tr.Content, `"tool_name":"Bash"`) {
		t.Errorf("TOOL_CALL_RESULT content is not the raw execution result: %q", tr.Content)
	}

	wantTypes := map[aguievents.EventType]bool{
		aguievents.EventTypeMessagesSnapshot: false,
		aguievents.EventTypeTextMessageStart: false,
		aguievents.EventTypeToolCallStart:    false,
		aguievents.EventTypeToolCallArgs:     false,
		aguievents.EventTypeToolCallEnd:      false,
		aguievents.EventTypeStateSnapshot:    false,
		aguievents.EventTypeCustom:           false,
	}
	for _, ev := range events {
		if _, tracked := wantTypes[ev.Type()]; tracked {
			wantTypes[ev.Type()] = true
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("expected at least one %s event", typ)
		}
	}
}

// TestAguiEncoderRunError asserts the failure path terminates the stream with
// a RUN_ERROR carrying the run id.
func TestAguiEncoderRunError(t *testing.T) {
	var buf bytes.Buffer
	e := &aguiEncoder{w: &buf}

	e.emitRunStarted("session-1")
	e.emitRunError("agent panic: boom")

	events := decodeLines(t, buf.String())
	if err := aguievents.ValidateSequence(events); err != nil {
		t.Fatalf("event sequence invalid: %v", err)
	}
	runErr, ok := events[len(events)-1].(*aguievents.RunErrorEvent)
	if !ok {
		t.Fatalf("last event = %s, want RUN_ERROR", events[len(events)-1].Type())
	}
	if runErr.Message != "agent panic: boom" {
		t.Errorf("RUN_ERROR message = %q", runErr.Message)
	}
	if runErr.RunID() != e.runID {
		t.Errorf("RUN_ERROR runId = %q, want %q", runErr.RunID(), e.runID)
	}
}

func decodeLines(t *testing.T, out string) []aguievents.Event {
	t.Helper()
	var events []aguievents.Event
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		ev, err := aguievents.EventFromJSON([]byte(line))
		if err != nil {
			t.Fatalf("line does not decode as an AG-UI event: %v\n%s", err, line)
		}
		events = append(events, ev)
	}
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	return events
}

func findToolCallResult(t *testing.T, events []aguievents.Event, toolCallID string) *aguievents.ToolCallResultEvent {
	t.Helper()
	for _, ev := range events {
		if tr, ok := ev.(*aguievents.ToolCallResultEvent); ok && tr.ToolCallID == toolCallID {
			return tr
		}
	}
	t.Fatalf("no TOOL_CALL_RESULT for %s", toolCallID)
	return nil
}
