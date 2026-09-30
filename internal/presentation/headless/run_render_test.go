package headless

import (
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computer "github.com/inference-gateway/cli/internal/computer"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// noticeEvent is a chat event the encoder does not map itself.
type noticeEvent struct {
	Name string
}

func (noticeEvent) GetRequestID() string    { return "" }
func (noticeEvent) GetTimestamp() time.Time { return time.Time{} }

// publishNotice publishes every notice's name as its own activity entry.
func publishNotice(event agentdomain.ChatEvent) (agui.Published, bool) {
	notice, ok := event.(noticeEvent)
	if !ok {
		return agui.Published{}, false
	}
	return agui.Published{ActivityType: notice.Name, MessageID: notice.Name, Content: map[string]bool{"active": true}}, true
}

// renderRun renders a fresh run, one without a restored history or boot notes.
func renderRun(events <-chan agentdomain.ChatEvent, w io.Writer, approvals <-chan ipc.ApprovalResponse, questions <-chan ipc.UserQuestionResponse, sessionID, model string, repo convdomain.ConversationRepository, jobs func() []scheddomain.TrackedJob, publish ...Publish) error {
	return renderAGUI(events, w, approvals, questions, nil, sessionID, model, repo, nil, jobs, nil, publish...)
}

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

// statePatch is one JSON Patch operation of a STATE_DELTA.
type statePatch struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value"`
}

// patches decodes the patch list a STATE_DELTA applies.
func patches(t *testing.T, ev wireEvent) []statePatch {
	t.Helper()
	var list []statePatch
	if err := json.Unmarshal(ev.Delta, &list); err != nil {
		t.Fatalf("STATE_DELTA delta is not a JSON Patch list: %v\n%s", err, ev.Delta)
	}
	return list
}

// textOf unwraps the string delta of a text or reasoning content event.
func textOf(t *testing.T, ev wireEvent) string {
	t.Helper()
	var text string
	if err := json.Unmarshal(ev.Delta, &text); err != nil {
		t.Fatalf("%s delta is not a string: %v\n%s", ev.Type, err, ev.Delta)
	}
	return text
}

func TestRender_SingleRunLifecycle(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
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
		t.Fatalf("renderRun() err = %v", err)
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

func TestRender_UserMessageIsFramedWithUserRole(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.UserMessageChatEvent{Content: "what's up"},
		agentdomain.ChatStartEvent{},
		agentdomain.ChatChunkEvent{RequestID: "r1", Content: "not much"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"role":"user"`) || !strings.Contains(got, `"delta":"what's up"`) {
		t.Errorf("user message not framed with role user\n%s", got)
	}
	if n := strings.Count(got, `"TEXT_MESSAGE_END"`); n != 2 {
		t.Errorf("TEXT_MESSAGE_END count = %d, want 2 (user + assistant)\n%s", n, got)
	}
}

func TestRender_BackgroundJobsAreStreamed(t *testing.T) {
	jobs := func() []scheddomain.TrackedJob {
		return []scheddomain.TrackedJob{
			{Meta: scheddomain.JobMeta{ID: "task-1", Kind: scheddomain.JobKindA2A, Label: "delay", Description: "call delay", Detail: "http://localhost:8081"}, Status: scheddomain.JobRunning},
			{Meta: scheddomain.JobMeta{ID: "sh-1", Kind: scheddomain.JobKindShell}, Status: scheddomain.JobCompleted},
		}
	}
	note := "[A2A Task Completed: delay]\n\nslow done"
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "A2A_SubmitTask", ToolCallID: "c1", Success: true}}},
		agentdomain.MessageQueuedEvent{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(note)}},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, jobs)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	got := out.String()
	if n := strings.Count(got, `"path":"/backgroundTasks"`); n != 2 {
		t.Errorf("backgroundTasks patch count = %d, want 2 (after the tool result and the queued note)\n%s", n, got)
	}
	for _, want := range []string{`"running":1`, `"id":"task-1"`, `"kind":"a2a"`, `"description":"call delay"`} {
		if !strings.Contains(got, want) {
			t.Errorf("backgroundTasks snapshot missing %s\n%s", want, got)
		}
	}
	events := decodeEvents(t, got)
	var landed string
	for i, ev := range events {
		if ev.Type != "TEXT_MESSAGE_START" || ev.Role != string(sdk.User) {
			continue
		}
		if i+1 >= len(events) || events[i+1].Type != "TEXT_MESSAGE_CONTENT" {
			t.Fatalf("the landed note's message carries no content right after its start\n%s", got)
		}
		landed = textOf(t, events[i+1])
	}
	if landed != note {
		t.Errorf("the landed note streamed %q, want %q", landed, note)
	}
}

func TestRender_QueuedNoteSplitsAssistantTurns(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ChatChunkEvent{Content: "submitted, waiting"},
		agentdomain.MessageQueuedEvent{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("[A2A Task Completed: x]\n\nok")}},
		agentdomain.ChatChunkEvent{ReasoningContent: "note arrived"},
		agentdomain.ChatChunkEvent{Content: "it finished"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	var starts []wireEvent
	for _, ev := range decodeEvents(t, out.String()) {
		if ev.Type == "TEXT_MESSAGE_START" {
			starts = append(starts, ev)
		}
	}
	if len(starts) != 3 {
		t.Fatalf("TEXT_MESSAGE_START count = %d, want 3 (assistant turn, note, assistant turn)\n%s", len(starts), out.String())
	}
	if starts[1].Role != string(sdk.User) {
		t.Errorf("the queued note's message role = %q, want user\n%s", starts[1].Role, out.String())
	}
	if starts[2].Role != string(sdk.Assistant) {
		t.Errorf("the note split the turn, the next message role = %q, want assistant\n%s", starts[2].Role, out.String())
	}
}

func TestRender_NilJobsEmitsNoSnapshot(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ToolExecutionCompletedEvent{Results: []*agentdomain.ToolExecutionResult{{ToolName: "Read", ToolCallID: "c1", Success: true}}},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	for _, ev := range decodeEvents(t, out.String()) {
		if ev.Type != "STATE_DELTA" {
			continue
		}
		for _, p := range patches(t, ev) {
			if p.Path == "/backgroundTasks" {
				t.Errorf("nil jobs patched the backgroundTasks state\n%s", out.String())
			}
		}
	}
}

func TestRender_ErrorEmitsSingleRunError(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatErrorEvent{Error: errors.New("boom")},
	), &out, nil, nil, "session-1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err == nil {
		t.Fatal("renderRun() err = nil, want error")
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

func TestRender_ApprovalSuspendsTheRunAsAnInterrupt(t *testing.T) {
	respChan := make(chan agentdomain.ApprovalAction, 1)
	ev := agentdomain.ToolApprovalRequestedEvent{
		ToolCall:     sdk.ChatCompletionMessageToolCall{ID: "tc1", Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Bash"}},
		ResponseChan: respChan,
	}
	approvals := approvalsChan(ipc.ApprovalResponse{ToolCallID: "tc1", Approved: true})
	var out strings.Builder
	if err := renderRun(stream(ev, agentdomain.ChatCompleteEvent{}), &out, approvals, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil); err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	select {
	case got := <-respChan:
		if got != agentdomain.ApprovalApprove {
			t.Fatalf("approval action = %v, want ApprovalApprove", got)
		}
	default:
		t.Fatal("no approval action sent on ResponseChan")
	}
	events := decodeEvents(t, out.String())
	suspended := interruptAt(events)
	if suspended < 0 {
		t.Fatalf("the approval did not suspend the run as an interrupt:\n%s", out.String())
	}
	interrupts := events[suspended].Outcome.Interrupts
	if len(interrupts) != 1 || interrupts[0].ToolCallID != "tc1" || interrupts[0].Reason != agui.InterruptToolCall {
		t.Errorf("interrupts = %+v, want one tool_call interrupt for tc1", interrupts)
	}
	if strings.Contains(out.String(), `"name":"approval_request"`) {
		t.Errorf("a run-scoped approval wrote an approvals CUSTOM event, the contract resolves one by interrupts:\n%s", out.String())
	}
	if continuation := continuationAt(events, suspended+1); continuation < 0 {
		t.Fatalf("the answered interrupt did not continue the run:\n%s", out.String())
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
			err := renderRun(stream(ev, agentdomain.ChatCompleteEvent{}), &out, nil, tt.questions, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
			if err != nil {
				t.Fatalf("renderRun() err = %v", err)
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
			if !strings.Contains(out.String(), `"reason":"input_required"`) || !strings.Contains(out.String(), `"responseSchema"`) {
				t.Fatalf("the question did not suspend the run as an input_required interrupt:\n%s", out.String())
			}
		})
	}
}

func TestAgentStartupEmitter(t *testing.T) {
	notes := newStartupNotes()
	startupEmitter(notes)("research-agent", "PullingImage", "Pulling image", 3, 10)
	var out strings.Builder
	err := renderAGUI(stream(agentdomain.ChatCompleteEvent{}), &out, nil, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil, nil, notes)
	if err != nil {
		t.Fatalf("renderAGUI() err = %v", err)
	}
	events := decodeEvents(t, out.String())
	start := slices.IndexFunc(events, func(ev wireEvent) bool { return ev.Type == "RUN_STARTED" })
	activity := slices.IndexFunc(events, func(ev wireEvent) bool { return ev.Type == "ACTIVITY_SNAPSHOT" })
	if start < 0 || activity < 0 || start > activity {
		t.Fatalf("the boot note must surface as ACTIVITY_SNAPSHOT after RUN_STARTED:\n%s", out.String())
	}
	for _, want := range []string{`"agent_status"`, `"research-agent"`, `"state":"PullingImage"`, `"done":3`, `"total":10`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s in %s", want, out.String())
		}
	}
}

func TestEmitRunError(t *testing.T) {
	var out strings.Builder
	emitAGUIRunError(&out, errors.New("gateway down"))
	if !strings.Contains(out.String(), `"RUN_ERROR"`) || !strings.Contains(out.String(), "gateway down") {
		t.Fatalf("EmitRunError output = %q, want RUN_ERROR with the message", out.String())
	}
}

func TestRender_PublishesTheEventsItDoesNotMap(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		noticeEvent{Name: "paused"},
		noticeEvent{Name: "resumed"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil, publishNotice)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	got := out.String()
	if n := strings.Count(got, `"ACTIVITY_SNAPSHOT"`); n != 2 {
		t.Errorf("ACTIVITY_SNAPSHOT count = %d, want one per published notice\n%s", n, got)
	}
	if !strings.Contains(got, `"activityType":"paused"`) || !strings.Contains(got, `"activityType":"resumed"`) {
		t.Errorf("the notices did not surface as activities\n%s", got)
	}
	if strings.Contains(got, `"RUN_ERROR"`) {
		t.Errorf("published notices must not fail the run\n%s", got)
	}
}

func TestRender_SkipsAnEventNobodyPublishes(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(noticeEvent{Name: "paused"}, agentdomain.ChatCompleteEvent{}), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	if strings.Contains(out.String(), `"ACTIVITY_SNAPSHOT"`) {
		t.Errorf("an unpublished event reached the stream:\n%s", out.String())
	}
}

func TestRender_RunFinishedCarriesSessionStats(t *testing.T) {
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
	err := renderRun(stream(
		agentdomain.ChatChunkEvent{Content: "hi"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "openai/gpt-4o", repo, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}

	var finished wireEvent
	for _, ev := range decodeEvents(t, out.String()) {
		if ev.Type == "RUN_FINISHED" {
			finished = ev
		}
	}
	if finished.Type != "RUN_FINISHED" {
		t.Fatalf("no RUN_FINISHED on the wire:\n%s", out.String())
	}
	if len(finished.Usage) != 1 {
		t.Fatalf("RUN_FINISHED usage = %+v, want one entry\n%s", finished.Usage, out.String())
	}
	for key, want := range map[string]any{
		"model":             "openai/gpt-4o",
		"inputTokens":       float64(4821),
		"outputTokens":      float64(310),
		"cachedInputTokens": float64(3100),
	} {
		if got := finished.Usage[0][key]; got != want {
			t.Errorf("usage[%q] = %v, want %v", key, got, want)
		}
	}
	for key, want := range map[string]any{
		"totalToolCalls": float64(1),
		"cost":           float64(0.042),
		"contextWindow":  float64(200000),
	} {
		if got := finished.Result[key]; got != want {
			t.Errorf("result[%q] = %v, want %v", key, got, want)
		}
	}
	if _, ok := finished.Result["inputTokens"]; ok {
		t.Errorf("result must not carry the token counts, usage does\n%s", out.String())
	}

	var plain strings.Builder
	err = renderRun(stream(agentdomain.ChatCompleteEvent{}), &plain, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	for _, ev := range decodeEvents(t, plain.String()) {
		if ev.Type == "RUN_FINISHED" && (ev.Usage != nil || ev.Result != nil) {
			t.Errorf("zero-request run's RUN_FINISHED carries usage %v or result %v, want neither", ev.Usage, ev.Result)
		}
	}
}

func TestRender_TokenUsageStreamsPerStep(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetSessionTokensStub = func() convdomain.SessionTokenStats {
		return convdomain.SessionTokenStats{
			TotalInputTokens: 4821, TotalOutputTokens: 310, TotalCachedTokens: 3100,
			TotalTokens: 8231, RequestCount: 2, LastInputTokens: 4821,
		}
	}

	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ChatChunkEvent{Content: "two"},
		agentdomain.ChatCompleteEvent{},
		agentdomain.ChatChunkEvent{Content: "two"},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "session-1", "openai/gpt-4o", repo, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	got := out.String()
	var usage []statePatch
	for _, ev := range decodeEvents(t, got) {
		if ev.Type != "STATE_DELTA" {
			continue
		}
		for _, p := range patches(t, ev) {
			if p.Path == "/usage" {
				usage = append(usage, p)
			}
		}
	}
	if len(usage) != 2 {
		t.Fatalf("usage patch count = %d, want one per LLM step\n%s", len(usage), got)
	}
	step := usageEntry(t, usage[0].Value, got)
	for key, want := range map[string]any{
		"model":             "openai/gpt-4o",
		"inputTokens":       float64(4821),
		"outputTokens":      float64(310),
		"cachedInputTokens": float64(3100),
	} {
		if v := step[key]; v != want {
			t.Errorf("usage patch entry[%q] = %v, want %v\n%s", key, v, want, got)
		}
	}
	for _, name := range []string{"cost", "contextWindow", "totalToolCalls"} {
		if _, ok := step[name]; ok {
			t.Errorf("the usage patch must not carry %q, the terminal result does\n%s", name, got)
		}
	}

	var plain strings.Builder
	err = renderRun(stream(agentdomain.ChatCompleteEvent{}), &plain, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	if strings.Contains(plain.String(), `"path":"/usage"`) {
		t.Errorf("zero-request run must not patch the usage state:\n%s", plain.String())
	}
}

// usageEntry unwraps the per-model entry inside a usage patch value.
func usageEntry(t *testing.T, value any, got string) map[string]any {
	t.Helper()
	entries, ok := value.([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("usage patch value = %+v, want one per-model entry\n%s", value, got)
	}
	entry, ok := entries[0].(map[string]any)
	if !ok {
		t.Fatalf("usage entry = %+v, want an object\n%s", entries[0], got)
	}
	return entry
}

// wireActivity is the slice of an ACTIVITY_SNAPSHOT event the render tests
// assert on.
type wireActivity struct {
	MessageID    string         `json:"messageId"`
	ActivityType string         `json:"activityType"`
	Content      map[string]any `json:"content"`
}

// activitySnapshots decodes every ACTIVITY_SNAPSHOT of a rendered run.
func activitySnapshots(t *testing.T, out string) []wireActivity {
	t.Helper()
	var list []wireActivity
	for line := range strings.Lines(out) {
		var ev struct {
			Type string `json:"type"`
			wireActivity
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line is not one JSON event: %v\n%s", err, line)
		}
		if ev.Type == "ACTIVITY_SNAPSHOT" {
			list = append(list, ev.wireActivity)
		}
	}
	return list
}

func TestRender_ComputerUseActionsSurfaceAsActivity(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ComputerUseActionEvent{ToolCallID: "tc-pointer", Action: "click", X: 640, Y: 512, ScreenWidth: 1920, ScreenHeight: 1080},
		agentdomain.ComputerUseActionEvent{ToolCallID: "tc-keyboard", Action: "key", ScreenWidth: 1920, ScreenHeight: 1080},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil, computer.PublishedEvent)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	activities := activitySnapshots(t, out.String())
	if len(activities) != 2 {
		t.Fatalf("ACTIVITY_SNAPSHOT count = %d, want one per computer-use action:\n%s", len(activities), out.String())
	}
	pointer := activities[0]
	if pointer.MessageID != "computer_use:tc-pointer" || pointer.ActivityType != "computer_use" {
		t.Fatalf("pointer activity = %+v, want computer_use keyed computer_use:tc-pointer", pointer)
	}
	for key, want := range map[string]any{
		"toolCallId": "tc-pointer", "action": "click", "x": float64(640), "y": float64(512), "screenWidth": float64(1920), "screenHeight": float64(1080),
	} {
		if got := pointer.Content[key]; got != want {
			t.Errorf("pointer content[%q] = %v, want %v\n%s", key, got, want, out.String())
		}
	}
	keyboard := activities[1]
	if keyboard.MessageID != "computer_use:tc-keyboard" || keyboard.ActivityType != "computer_use" {
		t.Fatalf("keyboard activity = %+v, want computer_use keyed computer_use:tc-keyboard", keyboard)
	}
	for key, want := range map[string]any{
		"toolCallId": "tc-keyboard", "action": "key", "screenWidth": float64(1920), "screenHeight": float64(1080),
	} {
		if got := keyboard.Content[key]; got != want {
			t.Errorf("keyboard content[%q] = %v, want %v\n%s", key, got, want, out.String())
		}
	}
	for _, absent := range []string{"x", "y"} {
		if _, ok := keyboard.Content[absent]; ok {
			t.Errorf("the keyboard action carries %q on the wire, want it absent:\n%s", absent, out.String())
		}
	}
}

func TestRender_ScreenRecordingStateCarriesTheFrame(t *testing.T) {
	var out strings.Builder
	err := renderRun(stream(
		agentdomain.ScreenRecordingStatusEvent{Active: true, Path: "/recordings/a.mp4", RegionX: 128, RegionY: 96, RegionWidth: 512, RegionHeight: 384, FrameWidth: 1024, FrameHeight: 768},
		agentdomain.ScreenRecordingStatusEvent{Active: false},
		agentdomain.ChatCompleteEvent{},
	), &out, nil, nil, "s1", "m", &convmocks.FakeConversationRepository{}, nil, computer.PublishedEvent)
	if err != nil {
		t.Fatalf("renderRun() err = %v", err)
	}
	var screen []statePatch
	for _, ev := range decodeEvents(t, out.String()) {
		if ev.Type != "STATE_DELTA" {
			continue
		}
		for _, p := range patches(t, ev) {
			if p.Path == "/screenRecording" {
				screen = append(screen, p)
			}
		}
	}
	if len(screen) != 2 {
		t.Fatalf("screenRecording patch count = %d, want start and stop:\n%s", len(screen), out.String())
	}
	started, err := json.Marshal(screen[0].Value)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"active":true,"frameHeight":768,"frameWidth":1024,"path":"/recordings/a.mp4","region":{"height":384,"width":512,"x":128,"y":96}}`; string(started) != want {
		t.Errorf("the recording start patch carried %s, want %s", started, want)
	}
	stopped, err := json.Marshal(screen[1].Value)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"active":false}`; string(stopped) != want {
		t.Errorf("the recording stop patch carried %s, want %s", stopped, want)
	}
}
