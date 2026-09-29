package render

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
)

// stream feeds the given events into a closed channel, mimicking the engine
// closing the channel when the run ends.
func stream(events ...agentdomain.ChatEvent) <-chan agentdomain.ChatEvent {
	ch := make(chan agentdomain.ChatEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestRenderText_MultiTurn(t *testing.T) {
	var out strings.Builder
	err := RenderText(stream(
		agentdomain.ChatChunkEvent{Content: "turn one"},
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatChunkEvent{Content: "turn two"},
		agentdomain.ChatCompleteEvent{},
	), &out)
	if err != nil {
		t.Fatalf("RenderText() err = %v", err)
	}
	if got := out.String(); got != "turn one\nturn two\n" {
		t.Fatalf("RenderText() output = %q, want both turns", got)
	}
}

func TestRenderText_MaxTurns(t *testing.T) {
	var out strings.Builder
	err := RenderText(stream(agentdomain.ChatCompleteEvent{MaxTurnsReached: true}), &out)
	if !errors.Is(err, agentdomain.ErrMaxTurnsReached) {
		t.Fatalf("RenderText() err = %v, want ErrMaxTurnsReached", err)
	}
}

// queuedNoteTurns is a headless run that waited on a background job: the first
// turn ends without a ChatCompleteEvent, then the job's note is drained.
func queuedNoteTurns(note string) <-chan agentdomain.ChatEvent {
	return stream(
		agentdomain.ChatChunkEvent{Content: "submitted, waiting"},
		agentdomain.MessageQueuedEvent{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(note)}},
		agentdomain.ChatChunkEvent{Content: "it finished"},
		agentdomain.ChatCompleteEvent{},
	)
}

func TestRenderJSON_QueuedNoteSplitsAssistantTurns(t *testing.T) {
	var out strings.Builder
	note := "[A2A Task Completed: x]\n\nok"
	if err := RenderJSON(queuedNoteTurns(note), &out, nil, nil, "session-1", "", nil, &convmocks.FakeConversationRepository{}); err != nil {
		t.Fatalf("RenderJSON() err = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")[1:]
	want := [][2]string{{"assistant", "submitted, waiting"}, {"user", note}, {"assistant", "it finished"}}
	if len(lines) != len(want) {
		t.Fatalf("got %d message lines, want %d\n%s", len(lines), len(want), out.String())
	}
	for i, line := range lines {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if msg["role"] != want[i][0] || msg["content"] != want[i][1] {
			t.Errorf("line %d = %v/%q, want %v/%q", i, msg["role"], msg["content"], want[i][0], want[i][1])
		}
	}
}

func TestRenderText_QueuedNoteSplitsTurns(t *testing.T) {
	var out strings.Builder
	if err := RenderText(queuedNoteTurns("note"), &out); err != nil {
		t.Fatalf("RenderText() err = %v", err)
	}
	if got := out.String(); got != "submitted, waiting\nit finished\n" {
		t.Fatalf("RenderText() output = %q, want one line per turn", got)
	}
}

// nudgeBoundaryTurns is a post_stream continuation nudge: the text-only turn
// ends with a completion event (published by transitionToStreaming), then the
// nudged turn calls a tool before the final completion.
func nudgeBoundaryTurns() <-chan agentdomain.ChatEvent {
	return stream(
		agentdomain.ChatChunkEvent{Content: "Review complete. Here is my verdict."},
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "TodoWrite", Arguments: "{}"}},
		}},
		agentdomain.ChatCompleteEvent{},
	)
}

func TestRenderJSON_NudgeBoundarySplitsAssistantTurns(t *testing.T) {
	var out strings.Builder
	if err := RenderJSON(nudgeBoundaryTurns(), &out, nil, nil, "session-1", "", nil, &convmocks.FakeConversationRepository{}); err != nil {
		t.Fatalf("RenderJSON() err = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")[1:]
	want := [][2]string{{"assistant", "Review complete. Here is my verdict."}, {"assistant", ""}}
	if len(lines) != len(want) {
		t.Fatalf("got %d message lines, want %d\n%s", len(lines), len(want), out.String())
	}
	for i, line := range lines {
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("line %d is not JSON: %v", i, err)
		}
		if msg["role"] != want[i][0] || msg["content"] != want[i][1] {
			t.Errorf("line %d = %v/%q, want %v/%q", i, msg["role"], msg["content"], want[i][0], want[i][1])
		}
	}
}

func TestAgentStartupEmitter(t *testing.T) {
	var out strings.Builder
	emit := AgentStartupEmitter(&out, "json")
	if emit == nil {
		t.Fatal("json must have an emitter")
	}
	emit("browser-agent", "PullingImage", "Pulling image", 3, 10)
	got := out.String()
	for _, want := range []string{`"type":"agent_status"`, `"browser-agent"`, `"state":"PullingImage"`, `"done":3`, `"total":10`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
	if AgentStartupEmitter(&out, "text") != nil {
		t.Error("text format must have no emitter")
	}
}

// approvalsChan feeds the given responses into a closed channel, mimicking
// the headless control broker after stdin EOF.
func approvalsChan(resps ...ipc.ApprovalResponse) <-chan ipc.ApprovalResponse {
	ch := make(chan ipc.ApprovalResponse, len(resps))
	for _, r := range resps {
		ch <- r
	}
	close(ch)
	return ch
}

func TestAnswerApproval_RoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		approvals <-chan ipc.ApprovalResponse
		want      agentdomain.ApprovalAction
	}{
		{"approved", approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true}), agentdomain.ApprovalApprove},
		{"rejected", approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: false}), agentdomain.ApprovalReject},
		{"empty tool_call_id matches", approvalsChan(ipc.ApprovalResponse{Approved: true}), agentdomain.ApprovalApprove},
		{"skips stale tool_call_id", approvalsChan(
			ipc.ApprovalResponse{ToolCallID: "stale", Approved: true},
			ipc.ApprovalResponse{ToolCallID: "tc1", Approved: false},
		), agentdomain.ApprovalReject},
		{"only stale responses reject", approvalsChan(ipc.ApprovalResponse{ToolCallID: "stale", Approved: true}), agentdomain.ApprovalReject},
		{"closed broker rejects", approvalsChan(), agentdomain.ApprovalReject},
		{"nil broker rejects", nil, agentdomain.ApprovalReject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respChan := make(chan agentdomain.ApprovalAction, 1)
			ev := agentdomain.ToolApprovalRequestedEvent{
				ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1"},
				ResponseChan: respChan,
			}
			var out strings.Builder
			err := RenderJSON(stream(ev), &out, tt.approvals, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
			if err != nil {
				t.Fatalf("RenderJSON() err = %v", err)
			}
			select {
			case got := <-respChan:
				if got != tt.want {
					t.Fatalf("approval action = %v, want %v", got, tt.want)
				}
			default:
				t.Fatal("no approval action sent on ResponseChan")
			}
			if !strings.Contains(out.String(), `"approval_request"`) {
				t.Fatalf("approval_request line not emitted:\n%s", out.String())
			}
		})
	}
}

func TestApprovalAction(t *testing.T) {
	tests := []struct {
		name string
		resp ipc.ApprovalResponse
		want agentdomain.ApprovalAction
	}{
		{"approved once", ipc.ApprovalResponse{Approved: true}, agentdomain.ApprovalApprove},
		{"approved always", ipc.ApprovalResponse{Approved: true, Scope: "always"}, agentdomain.ApprovalAutoAccept},
		{"rejected", ipc.ApprovalResponse{}, agentdomain.ApprovalReject},
		{"always without approval rejects", ipc.ApprovalResponse{Scope: "always"}, agentdomain.ApprovalReject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ApprovalAction(tt.resp); got != tt.want {
				t.Errorf("ApprovalAction(%+v) = %v, want %v", tt.resp, got, tt.want)
			}
		})
	}
}

func TestRenderJSON_StreamsPerTurn(t *testing.T) {
	var out strings.Builder
	err := RenderJSON(stream(
		agentdomain.ChatChunkEvent{Content: "calling a tool"},
		agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
		}},
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "Bash", Success: true}}},
		agentdomain.ChatChunkEvent{Content: "all done"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
	if err != nil {
		t.Fatalf("RenderJSON() err = %v", err)
	}
	got := out.String()
	for _, want := range []string{`"calling a tool"`, `"tool_name":"Bash"`, `"all done"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in streamed output:\n%s", want, got)
		}
	}
	if first, second := strings.Index(got, `"calling a tool"`), strings.Index(got, `"tool_name":"Bash"`); first > second {
		t.Errorf("tool result emitted before its assistant turn:\n%s", got)
	}
}

func TestRenderJSONPretty_Multiline(t *testing.T) {
	var out strings.Builder
	err := RenderJSONPretty(stream(
		agentdomain.ChatChunkEvent{Content: "hello"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
	if err != nil {
		t.Fatalf("RenderJSONPretty() err = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "{\n  \"") {
		t.Fatalf("output not indented multiline JSON:\n%s", got)
	}
	if !strings.Contains(got, `"content": "hello"`) {
		t.Fatalf("missing assistant content:\n%s", got)
	}
}

func TestRenderJSON_MaxTurns(t *testing.T) {
	var out strings.Builder
	err := RenderJSON(stream(agentdomain.ChatCompleteEvent{MaxTurnsReached: true}), &out, nil, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
	if !errors.Is(err, agentdomain.ErrMaxTurnsReached) {
		t.Fatalf("RenderJSON() err = %v, want ErrMaxTurnsReached", err)
	}
}

func TestEmitPreRunError_MachineFormats(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"json", `"agent_error"`},
		{"json-pretty", `"agent_error"`},
		{"text", ""},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			var out strings.Builder
			EmitPreRunError(&out, tt.format, errors.New("gateway down"))
			if tt.want == "" {
				if out.Len() != 0 {
					t.Fatalf("text format must stay silent, got %q", out.String())
				}
				return
			}
			if !strings.Contains(out.String(), tt.want) || !strings.Contains(out.String(), "gateway down") {
				t.Fatalf("EmitPreRunError(%s) output = %q, want %s with the message", tt.format, out.String(), tt.want)
			}
		})
	}
}

func TestRenderJSON_ComputerUsePauseResume(t *testing.T) {
	var out strings.Builder
	err := RenderJSON(stream(
		agentdomain.ComputerUsePausedEvent{RequestID: "s1"},
		agentdomain.ChatCompleteEvent{Cancelled: true},
		agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
		agentdomain.ChatChunkEvent{Content: "back at it"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
	if err != nil {
		t.Fatalf("RenderJSON() err = %v, want nil after resumed run completes", err)
	}
	got := out.String()
	for _, want := range []string{`"computer_use_paused"`, `"computer_use_resumed"`, `"request_id":"s1"`, `"back at it"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in output:\n%s", want, got)
		}
	}
}

func TestToolContent_NoLegacyEnvelope(t *testing.T) {
	ok := toolContent(&agentdomain.ToolExecutionResult{ToolName: "Read", Success: true})
	if strings.HasPrefix(ok, "Result of tool call") || !strings.Contains(ok, `"tool_name":"Read"`) {
		t.Fatalf("success content = %q, want bare marshaled result", ok)
	}
	failed := toolContent(&agentdomain.ToolExecutionResult{ToolName: "Bash", Success: false, Error: "exit 1"})
	if failed != "exit 1" {
		t.Fatalf("failure content = %q, want bare error detail", failed)
	}
}
