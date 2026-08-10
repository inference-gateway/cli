package render

import (
	"errors"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// stream feeds the given events into a closed channel, mimicking the engine
// closing the channel when the run ends.
func stream(events ...domain.ChatEvent) <-chan domain.ChatEvent {
	ch := make(chan domain.ChatEvent, len(events))
	for _, e := range events {
		ch <- e
	}
	close(ch)
	return ch
}

func TestRenderText_MultiTurn(t *testing.T) {
	var out strings.Builder
	err := RenderText(stream(
		domain.ChatChunkEvent{Content: "turn one"},
		domain.ChatCompleteEvent{}, // per-turn completion must not stop rendering
		domain.ChatChunkEvent{Content: "turn two"},
		domain.ChatCompleteEvent{},
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
	err := RenderText(stream(domain.ChatCompleteEvent{MaxTurnsReached: true}), &out)
	if !errors.Is(err, domain.ErrMaxTurnsReached) {
		t.Fatalf("RenderText() err = %v, want ErrMaxTurnsReached", err)
	}
}

func TestRenderAGUI_SingleRunLifecycle(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		domain.ChatStartEvent{},
		domain.ChatChunkEvent{RequestID: "r1", Content: "hi"},
		domain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
		}},
		domain.ChatStartEvent{},
		domain.ChatChunkEvent{RequestID: "r1", Content: "done"},
		domain.ChatCompleteEvent{},
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
}

func TestRenderAGUI_ErrorEmitsSingleRunError(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		domain.ChatCompleteEvent{},
		domain.ChatErrorEvent{Error: errors.New("boom")},
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

func TestAnswerApproval_RoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		stdin string
		want  domain.ApprovalAction
	}{
		{"approved", `{"type":"approval_response","tool_call_id":"tc1","approved":true}` + "\n", domain.ApprovalApprove},
		{"rejected", `{"type":"approval_response","tool_call_id":"tc1","approved":false}` + "\n", domain.ApprovalReject},
		{"skips noise lines", "not json\n" + `{"type":"other"}` + "\n" + `{"type":"approval_response","approved":true}` + "\n", domain.ApprovalApprove},
		{"closed stdin rejects", "", domain.ApprovalReject},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respChan := make(chan domain.ApprovalAction, 1)
			ev := domain.ToolApprovalRequestedEvent{
				ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1"},
				ResponseChan: respChan,
			}
			var out strings.Builder
			err := RenderJSON(stream(ev), &out, strings.NewReader(tt.stdin), "s1", "m", nil, &mocks.FakeConversationRepository{})
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
	respChan := make(chan domain.ApprovalAction, 1)
	ev := domain.ToolApprovalRequestedEvent{
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		ResponseChan: respChan,
	}
	stdin := strings.NewReader(`{"type":"approval_response","tool_call_id":"tc1","approved":true}` + "\n")
	var out strings.Builder
	if err := RenderAGUI(stream(ev, domain.ChatCompleteEvent{}), &out, stdin, "s1", "m"); err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	select {
	case got := <-respChan:
		if got != domain.ApprovalApprove {
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
		domain.ChatCompleteEvent{Message: "calling a tool", ToolCalls: []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash", Arguments: `{"command":"ls"}`}},
		}},
		domain.ToolExecutionCompletedEvent{Results: []*domain.ToolExecutionResult{{ToolName: "Bash", Success: true}}},
		domain.ChatCompleteEvent{Message: "all done"},
	), &out, nil, "s1", "m", nil, &mocks.FakeConversationRepository{})
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

func TestRenderJSON_MaxTurns(t *testing.T) {
	var out strings.Builder
	err := RenderJSON(stream(domain.ChatCompleteEvent{MaxTurnsReached: true}), &out, nil, "s1", "m", nil, &mocks.FakeConversationRepository{})
	if !errors.Is(err, domain.ErrMaxTurnsReached) {
		t.Fatalf("RenderJSON() err = %v, want ErrMaxTurnsReached", err)
	}
}
