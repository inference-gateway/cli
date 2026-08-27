package directexec_test

import (
	"strings"
	"testing"
	"time"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
	tuimocks "github.com/inference-gateway/cli/tests/mocks/tui"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	directexec "github.com/inference-gateway/cli/internal/presentation/tui/directexec"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// TestHandleBashCommand_OutputVisibleToLLM guards the "LLM can't see !command
// output" fix: the user-bash- tool pair is dropped from LLM history (issue
// #474), so the output must also land in a Hidden user entry that
// BuildAgentMessagesFromEntries keeps.
func TestHandleBashCommand_OutputVisibleToLLM(t *testing.T) {
	toolSvc := &agentdomainmocks.FakeToolService{}
	toolSvc.ExecuteToolDirectReturns(&agentdomain.ToolExecutionResult{Success: true}, nil)

	repo := &convmocks.FakeConversationRepository{}
	repo.FormatToolResultForLLMReturns("hello world")

	sm := statemanager.NewStateManager(false)
	sm.SetAgentMode(agentdomain.AgentModeStandard)

	svc := directexec.NewService(directexec.Options{
		ToolService:      toolSvc,
		StateManager:     sm,
		ConversationRepo: repo,
		Listener:         &tuimocks.FakeChatEventListener{},
	})

	if cmd := svc.HandleBashCommand(`!echo "hello world"`); cmd == nil {
		t.Fatal("expected a command from HandleBashCommand")
	}

	drainBashEvents(t, svc)

	entries := make([]convdomain.ConversationEntry, 0, repo.AddMessageCallCount())
	for i := 0; i < repo.AddMessageCallCount(); i++ {
		entries = append(entries, repo.AddMessageArgsForCall(i))
	}

	var hidden *convdomain.ConversationEntry
	for i := range entries {
		if entries[i].Hidden {
			hidden = &entries[i]
			break
		}
	}
	if hidden == nil {
		t.Fatal("expected a Hidden user entry carrying the command output")
	}
	if got, _ := hidden.Message.Content.AsMessageContent0(); !strings.Contains(got, "hello world") || !strings.Contains(got, `echo "hello world"`) {
		t.Errorf("hidden entry must contain the command and its output, got: %q", got)
	}

	msgs := conversation.BuildAgentMessagesFromEntries(entries)
	var sawOutput, sawToolPair bool
	for _, m := range msgs {
		if content, err := m.Content.AsMessageContent0(); err == nil && strings.Contains(content, "hello world") {
			sawOutput = true
		}
		if m.ToolCallID != nil || m.ToolCalls != nil {
			sawToolPair = true
		}
	}
	if !sawOutput {
		t.Error("LLM message list must include the command output via the hidden entry")
	}
	if sawToolPair {
		t.Error("the synthesized user-bash- tool pair must stay filtered from LLM messages")
	}
}

// drainBashEvents reads the per-invocation bash event channel until the async
// goroutine closes it, so all conversation entries have been added.
func drainBashEvents(t *testing.T, svc *directexec.Service) {
	t.Helper()

	ch := svc.PendingBashChannel()
	if ch == nil {
		t.Fatal("expected a pending bash event channel after HandleBashCommand")
	}

	timeout := time.After(5 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-timeout:
			t.Fatal("timed out draining bash events (channel never closed)")
		}
	}
}
