package a2acoord

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	adk "github.com/inference-gateway/adk/types"

	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

// newCoordinator wires a Service with fake dependencies.
func newCoordinator() (*Service, *mocksdomain.FakeConversationRepository, *mocksdomain.FakeStateManager, *mocksdomain.FakeTaskRetentionService, *mocksdomain.FakeChatEventListener) {
	repo := &mocksdomain.FakeConversationRepository{}
	state := &mocksdomain.FakeStateManager{}
	retention := &mocksdomain.FakeTaskRetentionService{}
	listener := &mocksdomain.FakeChatEventListener{}

	svc := NewService(Options{
		ConversationRepo:     repo,
		StateManager:         state,
		TaskRetentionService: retention,
		Listener:             listener,
	})
	return svc, repo, state, retention, listener
}

// runCmds invokes each tea.Cmd in order and returns the produced messages.
// Skips cmds that return nil.
func runCmds(cmds []tea.Cmd) []tea.Msg {
	out := make([]tea.Msg, 0, len(cmds))
	for _, c := range cmds {
		if c == nil {
			continue
		}
		if msg := c(); msg != nil {
			out = append(out, msg)
		}
	}
	return out
}

func TestService_HandleTaskSubmitted(t *testing.T) {
	t.Run("emits working status with agent name and pumps listener when session active", func(t *testing.T) {
		svc, _, state, _, listener := newCoordinator()
		eventChan := make(chan domain.ChatEvent, 1)
		state.GetChatSessionReturns(&domain.ChatSession{
			RequestID:    "req-1",
			EventChannel: eventChan,
		})
		listener.ListenForChatEventsReturns(func() tea.Msg { return nil })

		cmds := svc.taskSubmittedCmds(domain.A2ATaskSubmittedEvent{
			RequestID: "req-1",
			AgentName: "weather-agent",
		})

		if len(cmds) != 2 {
			t.Fatalf("expected 2 cmds (status + listener), got %d", len(cmds))
		}

		msgs := runCmds(cmds[:1])
		status, ok := msgs[0].(domain.SetStatusEvent)
		if !ok {
			t.Fatalf("expected SetStatusEvent, got %T", msgs[0])
		}
		if status.Message != "A2A task submitted to weather-agent" {
			t.Errorf("unexpected status message: %q", status.Message)
		}
		if status.StatusType != domain.StatusWorking {
			t.Errorf("expected StatusWorking, got %v", status.StatusType)
		}
		if !status.Spinner {
			t.Errorf("expected spinner on for in-flight submission")
		}

		if listener.ListenForChatEventsCallCount() != 1 {
			t.Errorf("expected listener to be called once, got %d", listener.ListenForChatEventsCallCount())
		}
	})

	t.Run("omits listener cmd when no active chat session", func(t *testing.T) {
		svc, _, state, _, listener := newCoordinator()
		state.GetChatSessionReturns(nil)

		cmds := svc.taskSubmittedCmds(domain.A2ATaskSubmittedEvent{AgentName: "foo"})

		if len(cmds) != 1 {
			t.Fatalf("expected exactly one cmd (no listener), got %d", len(cmds))
		}
		if listener.ListenForChatEventsCallCount() != 0 {
			t.Errorf("listener should not be invoked when session is nil")
		}
	})
}

func TestService_HandleTaskCompleted(t *testing.T) {
	t.Run("retains task and emits formatted result when result holds A2ASubmitTaskResult", func(t *testing.T) {
		svc, repo, state, retention, _ := newCoordinator()
		state.GetChatSessionReturns(nil)
		repo.GetMessagesReturns(nil)
		task := &adk.Task{ID: "task-1"}

		event := domain.A2ATaskCompletedEvent{
			RequestID: "req-1",
			Result: domain.ToolExecutionResult{
				Data: tools.A2ASubmitTaskResult{
					TaskResult: "all done",
					AgentURL:   "https://agent.example",
					Task:       task,
				},
				Duration: 250 * time.Millisecond,
			},
		}

		cmds := svc.taskCompletedCmds(event)

		if retention.AddTaskCallCount() != 1 {
			t.Fatalf("expected task retention AddTask once, got %d", retention.AddTaskCallCount())
		}
		retained := retention.AddTaskArgsForCall(0)
		if retained.AgentURL != "https://agent.example" || retained.Task.ID != "task-1" {
			t.Errorf("unexpected retained task: %+v", retained)
		}

		msgs := runCmds(cmds)
		var foundContent bool
		for _, m := range msgs {
			if sce, ok := m.(domain.StreamingContentEvent); ok {
				if sce.Content == "all done" && !sce.Delta && sce.RequestID == "req-1" {
					foundContent = true
				}
			}
		}
		if !foundContent {
			t.Errorf("expected streaming-content event with 'all done', got %#v", msgs)
		}
	})

	t.Run("falls back to repo formatter when result has no A2ASubmitTaskResult", func(t *testing.T) {
		svc, repo, state, retention, _ := newCoordinator()
		state.GetChatSessionReturns(nil)
		repo.FormatToolResultForLLMReturns("[formatted-result-text]")

		event := domain.A2ATaskCompletedEvent{Result: domain.ToolExecutionResult{Data: "unrelated"}}

		cmds := svc.taskCompletedCmds(event)

		if retention.AddTaskCallCount() != 0 {
			t.Errorf("retention should not be invoked when result is not an A2ASubmitTaskResult")
		}
		if repo.FormatToolResultForLLMCallCount() != 1 {
			t.Errorf("expected fallback to FormatToolResultForLLM")
		}

		msgs := runCmds(cmds)
		var found bool
		for _, m := range msgs {
			if sce, ok := m.(domain.StreamingContentEvent); ok && sce.Content == "[formatted-result-text]" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected fallback content in messages, got %#v", msgs)
		}
	})
}

func TestService_HandleTaskFailed(t *testing.T) {
	t.Run("formats error with task result when present", func(t *testing.T) {
		svc, _, state, _, _ := newCoordinator()
		state.GetChatSessionReturns(nil)

		cmds := svc.taskFailedCmds(domain.A2ATaskFailedEvent{
			Error: "boom",
			Result: domain.ToolExecutionResult{
				Data: tools.A2ASubmitTaskResult{TaskResult: "partial output"},
			},
		})

		msgs := runCmds(cmds)
		want := "[A2A Task Failed]\n\npartial output"
		var found bool
		for _, m := range msgs {
			if sce, ok := m.(domain.StreamingContentEvent); ok && sce.Content == want {
				found = true
			}
		}
		if !found {
			t.Errorf("expected error content %q, got %#v", want, msgs)
		}
	})

	t.Run("falls back to repo formatter with error string when no task result", func(t *testing.T) {
		svc, repo, state, _, _ := newCoordinator()
		state.GetChatSessionReturns(nil)
		repo.FormatToolResultForLLMReturns("formatted body")

		cmds := svc.taskFailedCmds(domain.A2ATaskFailedEvent{
			Error:  "timeout",
			Result: domain.ToolExecutionResult{Data: nil},
		})

		msgs := runCmds(cmds)
		var found bool
		for _, m := range msgs {
			if sce, ok := m.(domain.StreamingContentEvent); ok &&
				sce.Content == "[A2A Task Failed]\n\nError: timeout\n\nformatted body" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected fall-back error content with formatted body, got %#v", msgs)
		}
	})
}

func TestService_HandleTaskStatusUpdate(t *testing.T) {
	t.Run("emits working status with state and message", func(t *testing.T) {
		svc, _, state, _, _ := newCoordinator()
		state.GetChatSessionReturns(nil)

		cmds := svc.taskStatusUpdateCmds(domain.A2ATaskStatusUpdateEvent{
			Status:  "running",
			Message: "fetching",
		})

		msgs := runCmds(cmds)
		if len(msgs) != 1 {
			t.Fatalf("expected one msg, got %d", len(msgs))
		}
		upd, ok := msgs[0].(domain.UpdateStatusEvent)
		if !ok {
			t.Fatalf("expected UpdateStatusEvent, got %T", msgs[0])
		}
		if upd.Message != "A2A task running: fetching" {
			t.Errorf("unexpected status message: %q", upd.Message)
		}
		if upd.StatusType != domain.StatusWorking {
			t.Errorf("expected StatusWorking, got %v", upd.StatusType)
		}
	})
}

func TestService_HandleTaskInputRequired(t *testing.T) {
	t.Run("emits warning status with input requirement message", func(t *testing.T) {
		svc, _, state, _, _ := newCoordinator()
		state.GetChatSessionReturns(nil)

		cmds := svc.taskInputRequiredCmds(domain.A2ATaskInputRequiredEvent{
			Message: "need API key",
		})

		msgs := runCmds(cmds)
		if len(msgs) != 1 {
			t.Fatalf("expected one msg, got %d", len(msgs))
		}
		status, ok := msgs[0].(domain.SetStatusEvent)
		if !ok {
			t.Fatalf("expected SetStatusEvent, got %T", msgs[0])
		}
		if status.Message != "⚠️  A2A task requires input: need API key" {
			t.Errorf("unexpected status message: %q", status.Message)
		}
		if status.Spinner {
			t.Errorf("input-required status should not spin")
		}
	})
}

func TestService_HandleToolCallExecuted(t *testing.T) {
	t.Run("emits working status naming the tool", func(t *testing.T) {
		svc, _, state, _, _ := newCoordinator()
		state.GetChatSessionReturns(nil)

		cmds := svc.toolCallExecutedCmds(domain.A2AToolCallExecutedEvent{
			ToolName: "Read",
		})

		msgs := runCmds(cmds)
		if len(msgs) != 1 {
			t.Fatalf("expected one msg, got %d", len(msgs))
		}
		status, ok := msgs[0].(domain.SetStatusEvent)
		if !ok {
			t.Fatalf("expected SetStatusEvent, got %T", msgs[0])
		}
		if status.Message != "A2A tool Read executed on gateway" {
			t.Errorf("unexpected status message: %q", status.Message)
		}
		if !status.Spinner {
			t.Errorf("tool-executed status should spin while next step is in flight")
		}
	})
}

func TestService_HandleTaskCompleted_NilTaskRetentionService(t *testing.T) {
	repo := &mocksdomain.FakeConversationRepository{}
	state := &mocksdomain.FakeStateManager{}
	listener := &mocksdomain.FakeChatEventListener{}
	state.GetChatSessionReturns(nil)
	repo.GetMessagesReturns(nil)

	svc := NewService(Options{
		ConversationRepo:     repo,
		StateManager:         state,
		TaskRetentionService: nil,
		Listener:             listener,
	})

	t.Run("does not panic when retention service is nil but result includes task", func(t *testing.T) {
		event := domain.A2ATaskCompletedEvent{
			Result: domain.ToolExecutionResult{
				Data: tools.A2ASubmitTaskResult{
					TaskResult: "ok",
					Task:       &adk.Task{ID: "t1"},
				},
			},
		}

		cmds := svc.taskCompletedCmds(event)
		msgs := runCmds(cmds)

		var found bool
		for _, m := range msgs {
			if sce, ok := m.(domain.StreamingContentEvent); ok && sce.Content == "ok" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected streaming content event with 'ok', got %#v", msgs)
		}
	})
}

// Public-surface smoke test: the sequenced public methods return non-nil cmds
// when properly wired. This guards against regressions where the public
// method silently returns nil and side effects vanish.
func TestService_PublicMethods_ReturnNonNilCmds(t *testing.T) {
	svc, _, state, _, _ := newCoordinator()
	state.GetChatSessionReturns(nil)

	cases := []struct {
		name string
		cmd  tea.Cmd
	}{
		{"HandleTaskSubmitted", svc.HandleTaskSubmitted(domain.A2ATaskSubmittedEvent{AgentName: "a"})},
		{"HandleTaskCompleted", svc.HandleTaskCompleted(domain.A2ATaskCompletedEvent{})},
		{"HandleTaskFailed", svc.HandleTaskFailed(domain.A2ATaskFailedEvent{Error: "e"})},
		{"HandleTaskStatusUpdate", svc.HandleTaskStatusUpdate(domain.A2ATaskStatusUpdateEvent{Status: "s"})},
		{"HandleTaskInputRequired", svc.HandleTaskInputRequired(domain.A2ATaskInputRequiredEvent{Message: "m"})},
		{"HandleToolCallExecuted", svc.HandleToolCallExecuted(domain.A2AToolCallExecutedEvent{ToolName: "t"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.cmd == nil {
				t.Errorf("expected non-nil cmd from %s", tc.name)
			}
		})
	}
}
