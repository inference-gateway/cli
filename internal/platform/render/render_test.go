package render

import (
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
		agentdomain.ChatCompleteEvent{}, // per-turn completion must not stop rendering
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

func TestRenderAGUI_SingleRunLifecycle(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatStartEvent{},
		agentdomain.ChatChunkEvent{RequestID: "r1", Content: "hi"},
		agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
		}},
		agentdomain.ChatStartEvent{},
		agentdomain.ChatChunkEvent{RequestID: "r1", Content: "done"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, "session-1", "openai/gpt-4o")
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	got := out.String()
	if n := strings.Count(got, `"RUN_STARTED"`); n != 1 {
		t.Errorf("RUN_STARTED count = %d, want exactly 1\n%s", n, got)
	}
	if n := strings.Count(got, `"RUN_FINISHED"`); n != 1 {
		t.Errorf("RUN_FINISHED count = %d, want exactly 1\n%s", n, got)
	}
	if !strings.Contains(got, `"TOOL_CALL_START"`) {
		t.Errorf("missing TOOL_CALL_START for per-turn tool calls\n%s", got)
	}
	for _, typ := range []string{`"TEXT_MESSAGE_START"`, `"TEXT_MESSAGE_END"`} {
		if n := strings.Count(got, typ); n != 2 {
			t.Errorf("%s count = %d, want one per turn (2)\n%s", typ, n, got)
		}
	}
}

func TestRenderAGUI_UserMessageIsFramedWithUserRole(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.UserMessageChatEvent{Content: "what's up"},
		agentdomain.ChatStartEvent{},
		agentdomain.ChatChunkEvent{RequestID: "r1", Content: "not much"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, "session-1", "")
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"role":"user"`) || !strings.Contains(got, `"delta":"what's up"`) {
		t.Errorf("user message not framed with role user\n%s", got)
	}
	if n := strings.Count(got, `"TEXT_MESSAGE_END"`); n != 2 {
		t.Errorf("TEXT_MESSAGE_END count = %d, want 2 (user + assistant)\n%s", n, got)
	}
}

func TestRenderAGUI_ErrorEmitsSingleRunError(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatErrorEvent{Error: errors.New("boom")},
	), &out, nil, "session-1", "m")
	if err == nil {
		t.Fatal("RenderAGUI() err = nil, want error")
	}
	got := out.String()
	if n := strings.Count(got, `"RUN_ERROR"`); n != 1 {
		t.Errorf("RUN_ERROR count = %d, want exactly 1\n%s", n, got)
	}
	if strings.Contains(got, `"RUN_FINISHED"`) {
		t.Errorf("errored run must not emit RUN_FINISHED\n%s", got)
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
			err := RenderJSON(stream(ev), &out, tt.approvals, "s1", "m", nil, &convmocks.FakeConversationRepository{})
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

func TestRenderAGUI_ApprovalRoundTrip(t *testing.T) {
	respChan := make(chan agentdomain.ApprovalAction, 1)
	ev := agentdomain.ToolApprovalRequestedEvent{
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		ResponseChan: respChan,
	}
	approvals := approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true})
	var out strings.Builder
	if err := RenderAGUI(stream(ev, agentdomain.ChatCompleteEvent{}), &out, approvals, "s1", "m"); err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	select {
	case got := <-respChan:
		if got != agentdomain.ApprovalApprove {
			t.Fatalf("approval action = %v, want ApprovalApprove", got)
		}
	default:
		t.Fatal("no approval action sent on ResponseChan")
	}
	if !strings.Contains(out.String(), `approval_request`) {
		t.Fatalf("approval_request event not emitted:\n%s", out.String())
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
	), &out, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
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
	), &out, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
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
	err := RenderJSON(stream(agentdomain.ChatCompleteEvent{MaxTurnsReached: true}), &out, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
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
		{"ag-ui", `"RUN_ERROR"`},
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
	), &out, nil, "s1", "m", nil, &convmocks.FakeConversationRepository{})
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

func TestRenderAGUI_ComputerUsePauseResume(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ComputerUsePausedEvent{RequestID: "s1"},
		agentdomain.ChatCompleteEvent{Cancelled: true},
		agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, "s1", "m")
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v, want nil after resumed run completes", err)
	}
	got := out.String()
	for _, want := range []string{`computer_use_paused`, `computer_use_resumed`, `"CUSTOM"`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in output:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"RUN_ERROR"`) {
		t.Errorf("resumed run must not emit RUN_ERROR\n%s", got)
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
