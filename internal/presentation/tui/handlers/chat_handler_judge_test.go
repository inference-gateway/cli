package handlers

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

// TestHandleJudgeVerdictChatEvent_RearmsListener pins that the judge verdict
// handler keeps listening on the chat channel: without the re-arm the TUI
// silently drops every event after the first verdict (tool result, next
// turn, completion) and the tool card stays "queued" forever.
func TestHandleJudgeVerdictChatEvent_RearmsListener(t *testing.T) {
	for _, decision := range []agentdomain.JudgeDecision{agentdomain.JudgeDecisionApproved, agentdomain.JudgeDecisionRejected} {
		t.Run(string(decision), func(t *testing.T) {
			sm := statemanager.NewStateManager(false)
			events := make(chan agentdomain.ChatEvent, 1)
			if err := sm.StartChatSession("req-1", "test/model", events); err != nil {
				t.Fatalf("StartChatSession: %v", err)
			}
			h := &ChatHandler{stateManager: sm}

			cmd := h.HandleJudgeVerdictChatEvent(agentdomain.JudgeVerdictChatEvent{Decision: decision, Reason: "why"})
			if cmd == nil {
				t.Fatal("handler returned nil cmd: chat listener not re-armed")
			}

			next := agentdomain.JudgeVerdictChatEvent{Reason: "next"}
			events <- next
			if !cmdYields(cmd(), next) {
				t.Fatalf("returned cmds did not read the next chat event; listener not re-armed")
			}
		})
	}
}

// cmdYields reports whether msg, or any cmd inside a tea.BatchMsg, produces want.
func cmdYields(msg tea.Msg, want tea.Msg) bool {
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			if c != nil && cmdYields(c(), want) {
				return true
			}
		}
		return false
	case agentdomain.JudgeVerdictChatEvent:
		w, ok := want.(agentdomain.JudgeVerdictChatEvent)
		return ok && m.Reason == w.Reason
	default:
		return false
	}
}
