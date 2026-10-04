package a2a

import (
	"context"
	"sync"
	"testing"
	"time"

	adkmocks "github.com/inference-gateway/cli/tests/mocks/adk"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	baggage "go.opentelemetry.io/otel/baggage"
	trace "go.opentelemetry.io/otel/trace"

	adk "github.com/inference-gateway/adk/types"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	jobs "github.com/inference-gateway/cli/internal/scheduler/jobs"
)

// TestA2AJob_PollsRemoteTaskToCompletion drives the migrated A2A path end-to-end:
// the supervisor runs a2aJob.Run (the folded polling loop), which queries the
// remote agent, sees a completed task, returns the result, and stops polling -
// and the supervisor enqueues exactly one completion notification and hands
// the result to Finished, which retains the task for the task view.
func TestA2AJob_PollsRemoteTaskToCompletion(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools:   config.A2AToolsConfig{SubmitTask: config.SubmitTaskToolConfig{Enabled: true}},
			Task:    config.A2ATaskConfig{StatusPollSeconds: 1},
		},
	}

	tracker := NewTaskTracker(nil)
	queue := &convmocks.FakeMessageQueue{}
	sup := jobs.NewSupervisor(queue, &convmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	completed := adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateCompleted}}
	mockClient := &adkmocks.FakeA2AClient{}
	mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: completed}, nil)

	retention := NewTaskRetentionService(10)
	tool := NewSubmitTaskToolWithClient(cfg, tracker, sup, retention, mockClient)
	state := &a2adomain.TaskPollingState{TaskID: "t1", AgentURL: "http://agent", StartedAt: time.Now()}
	tracker.StartPolling("t1", state)
	sup.Submit(&a2aJob{tool: tool, agentURL: "http://agent", taskID: "t1", state: state})

	deadline := time.Now().Add(5 * time.Second)
	for queue.EnqueueCallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := queue.EnqueueCallCount(); n != 1 {
		t.Fatalf("Enqueue called %d times, want 1", n)
	}
	if tracker.GetPollingState("t1") != nil {
		t.Fatalf("StopPolling was not called when the task completed")
	}
	if retained := retention.GetTasks(); len(retained) != 1 || retained[0].Task.ID != "t1" {
		t.Fatalf("retained tasks = %+v, want the completed t1", retained)
	}
}

// TestA2AJob_PollsUnderSubmitSpan verifies the polling loop re-attaches the
// submit span's trace context and baggage, so each remote poll carries the
// session traceparent instead of starting a fresh trace.
func TestA2AJob_PollsUnderSubmitSpan(t *testing.T) {
	cfg := &config.Config{
		A2A: config.A2AConfig{
			Enabled: true,
			Tools:   config.A2AToolsConfig{SubmitTask: config.SubmitTaskToolConfig{Enabled: true}},
			Task:    config.A2ATaskConfig{StatusPollSeconds: 1},
		},
	}
	tracker := NewTaskTracker(nil)
	queue := &convmocks.FakeMessageQueue{}
	sup := jobs.NewSupervisor(queue, &convmocks.FakeConversationRepository{}, nil)
	defer sup.Stop()

	completed := adk.Task{ID: "t1", Status: adk.TaskStatus{State: adk.TaskStateCompleted}}
	mockClient := &adkmocks.FakeA2AClient{}
	mockClient.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: completed}, nil)

	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: trace.FlagsSampled,
	})
	member, _ := baggage.NewMember("session.id", "s1")
	bag, _ := baggage.New(member)

	tool := NewSubmitTaskToolWithClient(cfg, tracker, sup, nil, mockClient)
	state := &a2adomain.TaskPollingState{TaskID: "t1", AgentURL: "http://agent", StartedAt: time.Now()}
	tracker.StartPolling("t1", state)
	sup.Submit(&a2aJob{tool: tool, agentURL: "http://agent", taskID: "t1", state: state, spanCtx: spanCtx, bag: bag})

	deadline := time.Now().Add(5 * time.Second)
	for mockClient.GetTaskCallCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if mockClient.GetTaskCallCount() == 0 {
		t.Fatal("GetTask was never called")
	}
	var pollCtx context.Context
	pollCtx, _ = mockClient.GetTaskArgsForCall(0)
	if got := trace.SpanContextFromContext(pollCtx); got.TraceID() != spanCtx.TraceID() || got.SpanID() != spanCtx.SpanID() {
		t.Fatalf("poll context span = %v, want %v", got, spanCtx)
	}
	if got := baggage.FromContext(pollCtx).Member("session.id").Value(); got != "s1" {
		t.Fatalf("poll context baggage session.id = %q, want s1", got)
	}
}

// TestA2AJob_RetainedTask covers what Finished retains. Completed and failed
// carry the full *adk.Task, canceled is rebuilt from the polling state, and
// input-required and non-A2A results opt out. The asserted fields are exactly
// what the task view reads off a retained TaskInfo.
func TestA2AJob_RetainedTask(t *testing.T) {
	started := time.Now().Add(-2 * time.Minute)
	ctx1, ctx2 := "ctx1", "ctx2"
	fullTask := &adk.Task{ID: "t1", ContextID: &ctx1, Status: adk.TaskStatus{State: adk.TaskStateCompleted}}

	tests := []struct {
		name      string
		data      any
		wantOK    bool
		wantState adk.TaskState
		wantID    string
		wantCtx   string
		wantURL   string
	}{
		{
			name:      "completed carries the full task",
			data:      SubmitTaskResult{TaskID: "t1", ContextID: "ctx1", AgentURL: "http://agent", State: string(adk.TaskStateCompleted), Task: fullTask},
			wantOK:    true,
			wantState: adk.TaskStateCompleted,
			wantID:    "t1", wantCtx: "ctx1", wantURL: "http://agent",
		},
		{
			name:      "failed carries the full task",
			data:      SubmitTaskResult{TaskID: "t2", ContextID: "ctx2", AgentURL: "http://agent", State: string(adk.TaskStateFailed), Task: &adk.Task{ID: "t2", ContextID: &ctx2, Status: adk.TaskStatus{State: adk.TaskStateFailed}}},
			wantOK:    true,
			wantState: adk.TaskStateFailed,
			wantID:    "t2", wantCtx: "ctx2", wantURL: "http://agent",
		},
		{
			name:      "canceled without task is reconstructed from state",
			data:      SubmitTaskResult{TaskID: "t3", ContextID: "ctx3", AgentURL: "http://agent", State: string(adk.TaskStateCanceled)},
			wantOK:    true,
			wantState: adk.TaskStateCanceled,
			wantID:    "t3", wantCtx: "ctx3", wantURL: "http://agent",
		},
		{
			name:   "input-required is not retained",
			data:   SubmitTaskResult{TaskID: "t4", AgentURL: "http://agent", State: string(adk.TaskStateInputRequired)},
			wantOK: false,
		},
		{
			name:   "non-submit data is not retained",
			data:   "unrelated",
			wantOK: false,
		},
		{
			name:   "empty task id is not retained",
			data:   SubmitTaskResult{TaskID: "", State: string(adk.TaskStateCompleted)},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			j := &a2aJob{state: &a2adomain.TaskPollingState{StartedAt: started}}
			info, ok := j.retainedTask(agentdomain.ToolExecutionResult{Data: tt.data})
			if ok != tt.wantOK {
				t.Fatalf("retainedTask ok = %v, want %v", ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if info.Task.Status.State != tt.wantState {
				t.Errorf("state = %q, want %q", info.Task.Status.State, tt.wantState)
			}
			if info.Task.ID != tt.wantID {
				t.Errorf("task id = %q, want %q", info.Task.ID, tt.wantID)
			}
			if got := info.Task.GetContextID(); got != tt.wantCtx {
				t.Errorf("context id = %q, want %q", got, tt.wantCtx)
			}
			if info.AgentURL != tt.wantURL {
				t.Errorf("agent url = %q, want %q", info.AgentURL, tt.wantURL)
			}
			if !info.StartedAt.Equal(started) {
				t.Errorf("started at = %v, want %v", info.StartedAt, started)
			}
			if info.CompletedAt.IsZero() {
				t.Error("completed at should be set")
			}
		})
	}
}

// TestRetainableA2AState exercises the state matcher against both the prefixed adk
// enum (TASK_STATE_*) and bare/alternate spellings a remote might report.
func TestRetainableA2AState(t *testing.T) {
	tests := []struct {
		state adk.TaskState
		want  bool
	}{
		{adk.TaskStateCompleted, true},
		{adk.TaskStateFailed, true},
		{adk.TaskStateCanceled, true},
		{"completed", true},
		{"canceled", true},
		{"CANCELLED", true},
		{adk.TaskStateInputRequired, false},
		{adk.TaskStateWorking, false},
		{adk.TaskStateSubmitted, false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := retainableA2AState(tt.state); got != tt.want {
				t.Errorf("retainableA2AState(%q) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}

// TestA2AJob_PollingState asserts PollingState composes the immutable
// identity fields with the mutex-guarded last known state, and is nil-state safe.
func TestA2AJob_PollingState(t *testing.T) {
	started := time.Now().Add(-time.Minute)
	j := &a2aJob{
		taskID:   "t1",
		agentURL: "http://agent",
		state:    &a2adomain.TaskPollingState{ContextID: "ctx1", TaskDescription: "do work", StartedAt: started},
	}
	j.recordState(string(adk.TaskStateWorking))

	got := j.PollingState()
	if got.TaskID != "t1" || got.AgentURL != "http://agent" || got.ContextID != "ctx1" ||
		got.TaskDescription != "do work" || got.LastKnownState != string(adk.TaskStateWorking) {
		t.Errorf("PollingState = %+v, missing composed fields", got)
	}
	if !got.StartedAt.Equal(started) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, started)
	}
	if !got.IsPolling {
		t.Error("IsPolling should be true for a running job")
	}

	if nilState := (&a2aJob{taskID: "t2", agentURL: "http://a"}).PollingState(); nilState.TaskID != "t2" || nilState.ContextID != "" {
		t.Errorf("nil-state PollingState = %+v, want minimal without panic", nilState)
	}
}

// TestA2AJob_PollingStateConcurrent runs the guarded write (recordState, the
// poll goroutine's path) against the read (PollingState, the task view's path)
// so `go test -race` proves the mutex closes the shared-state data race.
func TestA2AJob_PollingStateConcurrent(t *testing.T) {
	j := &a2aJob{taskID: "t1", agentURL: "http://a", state: &a2adomain.TaskPollingState{ContextID: "ctx1", StartedAt: time.Now()}}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			j.recordState("working")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			_ = j.PollingState()
		}
	}()
	wg.Wait()
}

// TestA2AJobStatsFollowTaskMetadata: the job's stats are the usage the agent
// last attached to the task, and a poll without metadata keeps the last ones.
func TestA2AJobStatsFollowTaskMetadata(t *testing.T) {
	j := &a2aJob{taskID: "t1", agentURL: "http://a"}
	if got := j.Stats(); got != nil {
		t.Fatalf("stats before any poll = %+v, want nil", got)
	}

	j.recordStats(adk.Task{ID: "t1"})
	if got := j.Stats(); got != nil {
		t.Fatalf("stats without metadata = %+v, want nil", got)
	}

	metadata := map[string]any{
		adk.UsageMetadataKey:          map[string]any{"prompt_tokens": float64(1200), "completion_tokens": float64(80)},
		adk.ExecutionStatsMetadataKey: map[string]any{"tool_calls": float64(4), "failed_tools": float64(1)},
	}
	j.recordStats(adk.Task{ID: "t1", Metadata: &metadata})
	j.recordStats(adk.Task{ID: "t1"})

	want := scheddomain.SubagentRunStats{ToolsSucceeded: 3, ToolsFailed: 1, InputTokens: 1200, OutputTokens: 80}
	if got := j.Stats(); got == nil || *got != want {
		t.Fatalf("stats = %+v, want %+v", got, want)
	}
}

// TestA2AJobStatsWithoutUsage: an agent that reports execution stats but no
// usage gets its tool counts shown and no token figures.
func TestA2AJobStatsWithoutUsage(t *testing.T) {
	j := &a2aJob{taskID: "t1", agentURL: "http://a"}
	metadata := map[string]any{
		adk.ExecutionStatsMetadataKey: map[string]any{"failed_tools": float64(0), "iterations": float64(3), "messages": float64(1), "tool_calls": float64(1)},
	}
	j.recordStats(adk.Task{ID: "t1", Metadata: &metadata})

	got := j.Stats()
	if got == nil {
		t.Fatal("stats = nil, want the tool counts")
	}
	if want := "Tools: 1 succeeded, 0 failed"; got.String() != want {
		t.Fatalf("String() = %q, want %q", got.String(), want)
	}
}

// A wrap-up request resumes a task paused on input-required with the configured
// message and is refused, without touching the agent, in any other state.
func TestA2AJob_WindWrapUp(t *testing.T) {
	tests := []struct {
		name     string
		state    adk.TaskState
		wantSent bool
	}{
		{"input-required takes the message", adk.TaskStateInputRequired, true},
		{"working cannot receive input", adk.TaskStateWorking, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{A2A: config.A2AConfig{Enabled: true, Tools: config.A2AToolsConfig{SubmitTask: config.SubmitTaskToolConfig{Enabled: true}}}}
			cfg.Tools.Agent.WrapUpMessage = "wrap up"
			tracker := NewTaskTracker(nil)
			tracker.RegisterContext("http://agent", "ctx1")
			tracker.AddTask("ctx1", "t1")

			task := adk.Task{ID: "t1", ContextID: new("ctx1"), Status: adk.TaskStatus{State: tt.state}}
			client := &adkmocks.FakeA2AClient{}
			client.GetTaskReturns(&adk.JSONRPCSuccessResponse{Result: task}, nil)
			client.SendTaskReturns(&adk.JSONRPCSuccessResponse{Result: adk.SendMessageResponse{Task: &task}}, nil)

			tool := NewSubmitTaskToolWithClient(cfg, tracker, nil, nil, client)
			j := &a2aJob{tool: tool, agentURL: "http://agent", taskID: "t1", state: &a2adomain.TaskPollingState{ContextID: "ctx1"}}
			j.recordState(string(tt.state))

			err := j.Wind(t.Context(), scheddomain.WindWrapUp)
			if (err == nil) != tt.wantSent || (client.SendTaskCallCount() == 1) != tt.wantSent {
				t.Fatalf("Wind err=%v, sends=%d, want sent=%v", err, client.SendTaskCallCount(), tt.wantSent)
			}
			if !tt.wantSent {
				return
			}
			_, request := client.SendTaskArgsForCall(0)
			if got := request.Message; got.TaskID == nil || *got.TaskID != "t1" || len(got.Parts) != 1 {
				t.Fatalf("the message must resume t1, got %+v", got)
			}
		})
	}
}
