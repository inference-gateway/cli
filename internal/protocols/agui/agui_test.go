package agui

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
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
	), &out, nil, nil, "session-1", "openai/gpt-4o", &convmocks.FakeConversationRepository{}, nil)
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
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
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

func TestRenderAGUI_BackgroundJobsAreStreamed(t *testing.T) {
	var out strings.Builder
	jobs := func() []scheddomain.TrackedJob {
		return []scheddomain.TrackedJob{
			{Meta: scheddomain.JobMeta{ID: "task-1", Kind: scheddomain.JobKindA2A, Label: "delay", Description: "call delay", Detail: "http://localhost:8081"}, Status: scheddomain.JobRunning},
			{Meta: scheddomain.JobMeta{ID: "sh-1", Kind: scheddomain.JobKindShell}, Status: scheddomain.JobCompleted},
		}
	}
	note := "[A2A Task Completed: delay]\n\nslow done"
	err := RenderAGUI(stream(
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "A2A_SubmitTask", ToolCallID: "c1", Success: true}}},
		agentdomain.MessageQueuedEvent{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(note)}},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, jobs)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	got := out.String()
	if n := strings.Count(got, `"name":"background_tasks"`); n != 2 {
		t.Errorf("background_tasks count = %d, want 2 (after tool result and after queued note)\n%s", n, got)
	}
	if !strings.Contains(got, `"running":1`) || !strings.Contains(got, `"id":"task-1"`) || !strings.Contains(got, `"kind":"a2a"`) || !strings.Contains(got, `"description":"call delay"`) {
		t.Errorf("background_tasks snapshot missing running count or job fields\n%s", got)
	}
	var queued map[string]any
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, `"name":"queued_message"`) {
			if err := json.Unmarshal([]byte(line), &queued); err != nil {
				t.Fatalf("queued_message line is not JSON: %v", err)
			}
		}
	}
	if queued == nil {
		t.Fatalf("queued_message event missing\n%s", got)
	}
	if v, _ := queued["value"].(map[string]any); v["content"] != note {
		t.Errorf("queued_message content = %v, want the landed note", v["content"])
	}
}

func TestRenderAGUI_QueuedNoteSplitsAssistantTurns(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatChunkEvent{Content: "submitted, waiting"},
		agentdomain.MessageQueuedEvent{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("[A2A Task Completed: x]\n\nok")}},
		agentdomain.ChatChunkEvent{ReasoningContent: "note arrived"},
		agentdomain.ChatChunkEvent{Content: "it finished"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	got := out.String()
	if n := strings.Count(got, `"TEXT_MESSAGE_START"`); n != 2 {
		t.Errorf("TEXT_MESSAGE_START count = %d, want 2 (one assistant message per turn)\n%s", n, got)
	}
	first := strings.Index(got, `"TEXT_MESSAGE_END"`)
	note := strings.Index(got, `"name":"queued_message"`)
	if first < 0 || note < 0 || first > note {
		t.Errorf("first assistant message must end before the queued note\n%s", got)
	}
}

func TestRenderAGUI_NilJobsEmitsNoSnapshot(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "Read", ToolCallID: "c1", Success: true}}},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	if strings.Contains(out.String(), "background_tasks") {
		t.Errorf("nil jobs must not emit background_tasks\n%s", out.String())
	}
}

func TestRenderAGUI_ErrorEmitsSingleRunError(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatErrorEvent{Error: errors.New("boom")},
	), &out, nil, nil, "session-1", "m", &convmocks.FakeConversationRepository{}, nil)
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

func TestRenderAGUI_ApprovalRoundTrip(t *testing.T) {
	respChan := make(chan agentdomain.ApprovalAction, 1)
	ev := agentdomain.ToolApprovalRequestedEvent{
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		ResponseChan: respChan,
	}
	approvals := approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true})
	var out strings.Builder
	if err := RenderAGUI(stream(ev, agentdomain.ChatCompleteEvent{}), &out, approvals, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil); err != nil {
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

func questionsChan(resps ...ipc.UserQuestionResponse) <-chan ipc.UserQuestionResponse {
	ch := make(chan ipc.UserQuestionResponse, len(resps))
	for _, r := range resps {
		ch <- r
	}
	close(ch)
	return ch
}

func TestAnswerQuestions_RoundTrip(t *testing.T) {
	answered := json.RawMessage(`[{"header":"Lang","question":"Which?","selectedLabels":["Go"],"otherText":""}]`)
	tests := []struct {
		name      string
		questions <-chan ipc.UserQuestionResponse
		want      []string
		dismissed bool
	}{
		{"answered", questionsChan(ipc.UserQuestionResponse{ToolCallID: "tc1", Answers: answered}), []string{"Go"}, false},
		{"empty tool_call_id matches", questionsChan(ipc.UserQuestionResponse{Answers: answered}), []string{"Go"}, false},
		{"skips stale tool_call_id", questionsChan(
			ipc.UserQuestionResponse{ToolCallID: "stale", Cancelled: true},
			ipc.UserQuestionResponse{ToolCallID: "tc1", Answers: answered},
		), []string{"Go"}, false},
		{"cancelled dismisses", questionsChan(ipc.UserQuestionResponse{ToolCallID: "tc1", Cancelled: true}), nil, true},
		{"malformed answers dismiss", questionsChan(ipc.UserQuestionResponse{ToolCallID: "tc1", Answers: json.RawMessage(`"nope"`)}), nil, true},
		{"closed broker dismisses", questionsChan(), nil, true},
		{"nil broker dismisses", nil, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respChan := make(chan []agentdomain.UserQuestionAnswer, 1)
			ev := agentdomain.UserQuestionRequestedEvent{
				ToolCallID:   "tc1",
				Questions:    []agentdomain.UserQuestion{{Header: "Lang", Question: "Which?", Options: []agentdomain.UserQuestionOption{{Label: "Go"}, {Label: "Rust"}}}},
				ResponseChan: respChan,
			}
			var out strings.Builder
			err := RenderAGUI(stream(ev, agentdomain.ChatCompleteEvent{}), &out, nil, tt.questions, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
			if err != nil {
				t.Fatalf("RenderAGUI() err = %v", err)
			}
			answers, open := <-respChan
			if tt.dismissed {
				if open {
					t.Fatalf("expected dismissal (closed channel), got answers %+v", answers)
				}
			} else {
				if !open || len(answers) != 1 || strings.Join(answers[0].SelectedLabels, ",") != strings.Join(tt.want, ",") {
					t.Fatalf("answers = %+v (open=%v), want labels %v", answers, open, tt.want)
				}
			}
			if !strings.Contains(out.String(), `"user_question_request"`) || !strings.Contains(out.String(), `"tool_call_id":"tc1"`) || !strings.Contains(out.String(), `"label":"Rust"`) {
				t.Fatalf("user_question_request line not emitted with payload:\n%s", out.String())
			}
		})
	}
}

func TestAgentStartupEmitter(t *testing.T) {
	var out strings.Builder
	emit := AgentStartupEmitter(&out)
	emit("browser-agent", "PullingImage", "Pulling image", 3, 10)
	got := out.String()
	for _, want := range []string{`"type":"CUSTOM"`, `"name":"agent_status"`, `"browser-agent"`, `"state":"PullingImage"`, `"done":3`, `"total":10`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in %s", want, got)
		}
	}
}

func TestEmitRunError(t *testing.T) {
	var out strings.Builder
	EmitRunError(&out, "gateway down")
	if !strings.Contains(out.String(), `"RUN_ERROR"`) || !strings.Contains(out.String(), "gateway down") {
		t.Fatalf("EmitRunError output = %q, want RUN_ERROR with the message", out.String())
	}
}

func TestRenderAGUI_ComputerUsePauseResume(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ComputerUsePausedEvent{RequestID: "s1"},
		agentdomain.ChatCompleteEvent{Cancelled: true},
		agentdomain.ComputerUseResumedEvent{RequestID: "s1"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
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

func TestRenderAGUI_RunFinishedCarriesSessionStats(t *testing.T) {
	config.UserContextWindows = map[string]int{"gpt-4o": 200000}
	t.Cleanup(func() { config.UserContextWindows = nil })

	repo := &convmocks.FakeConversationRepository{}
	repo.GetSessionTokensStub = func() convdomain.SessionTokenStats {
		return convdomain.SessionTokenStats{
			TotalInputTokens: 4821, TotalOutputTokens: 310, TotalCachedTokens: 3100,
			TotalTokens: 8231, RequestCount: 2, LastInputTokens: 4821,
		}
	}
	repo.GetSessionCostStatsStub = func() convdomain.SessionCostStats {
		return convdomain.SessionCostStats{TotalCost: 0.042}
	}
	repo.GetMessagesStub = func() []convdomain.ConversationEntry {
		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		}
		return []convdomain.ConversationEntry{{Message: sdk.Message{Role: sdk.Assistant, ToolCalls: &toolCalls}}}
	}

	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatChunkEvent{Content: "hi"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "openai/gpt-4o", repo, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}

	var result map[string]any
	for line := range strings.SplitSeq(out.String(), "\n") {
		if !strings.Contains(line, `"RUN_FINISHED"`) {
			continue
		}
		var finished struct {
			Type   string         `json:"type"`
			Result map[string]any `json:"result"`
		}
		if err := json.Unmarshal([]byte(line), &finished); err != nil {
			t.Fatalf("RUN_FINISHED line is not valid JSON: %v\n%s", err, line)
		}
		if finished.Type != "RUN_FINISHED" {
			t.Fatalf("parsed type = %q, want RUN_FINISHED", finished.Type)
		}
		result = finished.Result
		break
	}
	if result == nil {
		t.Fatalf("RUN_FINISHED carries no result:\n%s", out.String())
	}
	for key, want := range map[string]float64{
		"inputTokens":     4821,
		"outputTokens":    310,
		"cacheReadTokens": 3100,
		"totalToolCalls":  1,
		"cost":            0.042,
		"lastInputTokens": 4821,
		"contextWindow":   200000,
	} {
		got, ok := result[key].(float64)
		if !ok || got != want {
			t.Errorf("result[%q] = %v, want %v", key, result[key], want)
		}
	}

	var plain strings.Builder
	err = RenderAGUI(stream(agentdomain.ChatCompleteEvent{}), &plain, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	if strings.Contains(plain.String(), `"result"`) {
		t.Errorf("zero-request run must emit RUN_FINISHED without result:\n%s", plain.String())
	}
}

func TestRenderAGUI_TokenUsageStreamsPerStep(t *testing.T) {
	config.UserContextWindows = map[string]int{"gpt-4o": 200000}
	t.Cleanup(func() { config.UserContextWindows = nil })

	repo := &convmocks.FakeConversationRepository{}
	repo.GetSessionTokensStub = func() convdomain.SessionTokenStats {
		return convdomain.SessionTokenStats{
			TotalInputTokens: 4821, TotalOutputTokens: 310, TotalCachedTokens: 3100,
			TotalTokens: 8231, RequestCount: 2, LastInputTokens: 4821,
		}
	}
	repo.GetSessionCostStatsStub = func() convdomain.SessionCostStats {
		return convdomain.SessionCostStats{TotalCost: 0.042}
	}
	repo.GetMessagesStub = func() []convdomain.ConversationEntry {
		toolCalls := []sdk.ChatCompletionMessageToolCall{
			{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		}
		return []convdomain.ConversationEntry{{Message: sdk.Message{Role: sdk.Assistant, ToolCalls: &toolCalls}}}
	}

	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ChatChunkEvent{Content: "one"},
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatChunkEvent{Content: "two"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "openai/gpt-4o", repo, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}

	got := out.String()
	if n := strings.Count(got, `"name":"token_usage"`); n != 2 {
		t.Errorf("token_usage event count = %d, want one per LLM step\n%s", n, got)
	}
	for line := range strings.SplitSeq(got, "\n") {
		if !strings.Contains(line, `"name":"token_usage"`) {
			continue
		}
		var ev struct {
			Type  string         `json:"type"`
			Name  string         `json:"name"`
			Value map[string]any `json:"value"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("token_usage line is not valid JSON: %v\n%s", err, line)
		}
		if ev.Type != "CUSTOM" || ev.Name != "token_usage" {
			t.Errorf("event type/name = %q/%q, want CUSTOM/token_usage", ev.Type, ev.Name)
		}
		for key, want := range map[string]float64{
			"inputTokens":     4821,
			"outputTokens":    310,
			"cacheReadTokens": 3100,
			"totalToolCalls":  1,
			"cost":            0.042,
			"lastInputTokens": 4821,
			"contextWindow":   200000,
		} {
			if v, ok := ev.Value[key].(float64); !ok || v != want {
				t.Errorf("token_usage value[%q] = %v, want %v", key, ev.Value[key], want)
			}
		}
	}

	var plain strings.Builder
	err = RenderAGUI(stream(agentdomain.ChatCompleteEvent{}), &plain, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	if strings.Contains(plain.String(), `"name":"token_usage"`) {
		t.Errorf("zero-request run must not emit token_usage:\n%s", plain.String())
	}
}

func TestRenderAGUI_ScreenRecordingStatus(t *testing.T) {
	var out strings.Builder
	err := RenderAGUI(stream(
		agentdomain.ScreenRecordingStatusEvent{Active: true},
		agentdomain.ScreenRecordingStatusEvent{Active: false},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("RenderAGUI() err = %v", err)
	}
	got := out.String()
	for _, want := range []string{`"name":"screen_recording","value":{"active":true}`, `"name":"screen_recording","value":{"active":false}`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in output:\n%s", want, got)
		}
	}
}
