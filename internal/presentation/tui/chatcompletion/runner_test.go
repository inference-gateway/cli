package chatcompletion

import (
	"fmt"
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// newRunnerForTest wires a Runner with the in-memory conversation repository
// and counterfeiter fakes for everything else.
func newRunnerForTest() (*Runner, *conversation.InMemoryConversationRepository, *statemanager.StateManager, *agentdomainmocks.FakeAgentService, *convmocks.FakeModelService) {
	repo := conversation.NewInMemoryConversationRepository(nil, nil)
	state := statemanager.NewStateManager(false)
	agent := &agentdomainmocks.FakeAgentService{}
	model := &convmocks.FakeModelService{}

	runner := NewRunner(Options{
		AgentService:     agent,
		ConversationRepo: repo,
		ModelService:     model,
		StateManager:     state,
	})
	return runner, repo, state, agent, model
}

func TestRunner_Start(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancel), func(t *testing.T) {
			runner, _, state, agent, model := newRunnerForTest()
			model.GetCurrentModelReturns("test/model")
			state.SetChatPending()
			pending := state.GetChatSession()
			events := make(chan agentdomain.ChatEvent)
			defer close(events)
			agent.RunWithStreamReturns(events, nil)

			cmd := runner.Start(nil)
			if cancel {
				state.EndChatSession()
			}
			before := state.GetChatSession()
			opened, ok := cmd().(tui.ChatStreamOpenedEvent)
			if !ok || opened.Err != nil || opened.Events != events || opened.Session != pending {
				t.Fatalf("unexpected stream result: %+v", opened)
			}
			if opened.RequestID == "" || opened.Model != "test/model" {
				t.Fatalf("missing request identity: %+v", opened)
			}
			if state.GetChatSession() != before {
				t.Fatal("startup command must not mutate or recreate the UI session")
			}
		})
	}

	t.Run("returns startup error when no model is selected", func(t *testing.T) {
		runner, _, _, _, model := newRunnerForTest()
		model.GetCurrentModelReturns("")
		opened, ok := runner.Start(nil)().(tui.ChatStreamOpenedEvent)
		if !ok || opened.Err == nil || !strings.Contains(opened.Err.Error(), "no model selected") {
			t.Fatalf("expected no model selected error, got %+v", opened)
		}
	})
}

// TestRunner_HandleStatusUpdate_EmitsThinkingOnLaterTurns is a regression test
// for issue #992: after the first turn (tool execution, IsFirstChunk consumed),
// a reasoning chunk must still flip the status line to "Thinking..." on later
// turns. Previously the comparison ran after UpdateChatStatus had already
// mutated chatSession.Status, so no status event was ever emitted and the
// spinner stayed on "Starting response..." for the whole thinking phase.
func TestRunner_HandleStatusUpdate_EmitsThinkingOnLaterTurns(t *testing.T) {
	t.Run("first chunk emits SetStatusEvent Thinking...", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		_ = state.StartChatSession("req-1", "model", make(chan agentdomain.ChatEvent))

		cmds := runner.handleStatusUpdate(agentdomain.ChatChunkEvent{
			RequestID:        "req-1",
			ReasoningContent: "thinking...",
		}, state.GetChatSession())

		if len(cmds) != 1 {
			t.Fatalf("expected 1 status cmd, got %d", len(cmds))
		}
		evt, ok := cmds[0]().(tui.SetStatusEvent)
		if !ok {
			t.Fatalf("expected SetStatusEvent, got %T", cmds[0]())
		}
		if evt.Message != "Thinking..." || evt.StatusType != tui.StatusThinking {
			t.Errorf("expected Thinking... status event, got %+v", evt)
		}
	})

	t.Run("later turn (IsFirstChunk false) emits UpdateStatusEvent Thinking...", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		_ = state.StartChatSession("req-1", "model", make(chan agentdomain.ChatEvent))

		session := state.GetChatSession()
		session.IsFirstChunk = false
		_ = state.UpdateChatStatus(tui.ChatStatusStarting)

		cmds := runner.handleStatusUpdate(agentdomain.ChatChunkEvent{
			RequestID:        "req-1",
			ReasoningContent: "thinking...",
		}, session)

		if len(cmds) != 1 {
			t.Fatalf("expected 1 status cmd, got %d", len(cmds))
		}
		evt, ok := cmds[0]().(tui.UpdateStatusEvent)
		if !ok {
			t.Fatalf("expected UpdateStatusEvent, got %T", cmds[0]())
		}
		if evt.Message != "Thinking..." || evt.StatusType != tui.StatusThinking {
			t.Errorf("expected Thinking... status event, got %+v", evt)
		}
	})
}

func TestRunner_HandleChatComplete(t *testing.T) {
	t.Run("non-cancelled, no tool calls: updates status to Completed and returns non-nil cmd", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		_ = state.StartChatSession("req-1", "model", make(chan agentdomain.ChatEvent))
		_ = state.UpdateChatStatus(tui.ChatStatusGenerating)
		_ = state.StartToolExecution([]sdk.ChatCompletionMessageToolCall{{ID: "tc"}})

		cmd := runner.HandleChatComplete(agentdomain.ChatCompleteEvent{
			RequestID: "req-1",
			Timestamp: time.Now(),
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if s := state.GetChatSession(); s == nil || s.Status != tui.ChatStatusCompleted {
			t.Errorf("expected chat status Completed, got %+v", s)
		}
		if state.GetToolExecution() != nil {
			t.Errorf("expected EndToolExecution to clear tool execution on terminal completion")
		}
	})

	t.Run("cancelled: tears down session and updates status to Cancelled", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		_ = state.StartChatSession("req-1", "model", make(chan agentdomain.ChatEvent))
		_ = state.StartToolExecution([]sdk.ChatCompletionMessageToolCall{{ID: "tc"}})

		cmd := runner.HandleChatComplete(agentdomain.ChatCompleteEvent{
			RequestID: "req-1",
			Cancelled: true,
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}

		if state.GetChatSession() != nil {
			t.Errorf("expected EndChatSession to clear the chat session on cancel")
		}
		if state.GetToolExecution() != nil {
			t.Errorf("expected EndToolExecution to clear tool execution on cancel")
		}
	})

	t.Run("with tool calls: transitions to WaitingTools to prevent false stall detection", func(t *testing.T) {
		runner, _, state, _, _ := newRunnerForTest()
		_ = state.StartChatSession("req-1", "model", make(chan agentdomain.ChatEvent))

		_ = runner.HandleChatComplete(agentdomain.ChatCompleteEvent{
			RequestID: "req-1",
			ToolCalls: []sdk.ChatCompletionMessageToolCall{{
				ID:   "tc-1",
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name: "Read", Arguments: `{}`,
				},
			}},
		})

		if s := state.GetChatSession(); s == nil || s.Status != tui.ChatStatusWaitingTools {
			t.Errorf("expected chat status WaitingTools, got %+v", s)
		}
	})
}

func TestRunner_SetPendingRestoration_RestoresOnComplete(t *testing.T) {
	t.Run("restoration runs SelectModel on next HandleChatComplete", func(t *testing.T) {
		runner, _, _, _, model := newRunnerForTest()

		runner.SetPendingRestoration("gpt-4")

		_ = runner.HandleChatComplete(agentdomain.ChatCompleteEvent{RequestID: "r"})

		if model.SelectModelCallCount() != 1 {
			t.Fatalf("expected SelectModel called once, got %d", model.SelectModelCallCount())
		}
		if got := model.SelectModelArgsForCall(0); got != "gpt-4" {
			t.Errorf("expected SelectModel(\"gpt-4\"), got %q", got)
		}

		_ = runner.HandleChatComplete(agentdomain.ChatCompleteEvent{RequestID: "r"})
		if model.SelectModelCallCount() != 1 {
			t.Errorf("expected SelectModel still 1 after second complete, got %d", model.SelectModelCallCount())
		}
	})
}
