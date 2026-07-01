package approvalcoord

import (
	"errors"
	"testing"

	domain "github.com/inference-gateway/cli/internal/domain"
	services "github.com/inference-gateway/cli/internal/services"
	mocksdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

// newCoordinator returns a Service wired with fake dependencies for tests.
// The conversation repo is an *InMemoryConversationRepository because the
// coordinator uses the concrete planRepoUpdater interface for plan-status
// mutations, which the in-memory repo satisfies.
func newCoordinator() (*Service, *services.InMemoryConversationRepository, *mocksdomain.FakeStateManager, *mocksdomain.FakeAgentService) {
	repo := services.NewInMemoryConversationRepository(nil, nil)
	state := &mocksdomain.FakeStateManager{}
	agent := &mocksdomain.FakeAgentService{}

	svc := NewService(Options{
		AgentService:     agent,
		ConversationRepo: repo,
		StateManager:     state,
	})
	return svc, repo, state, agent
}

func TestService_HandlePlanApprovalRequested(t *testing.T) {
	t.Run("sets up plan approval UI state and returns a non-nil cmd", func(t *testing.T) {
		svc, _, state, _ := newCoordinator()
		responseChan := make(chan domain.PlanApprovalAction, 1)

		cmd := svc.HandlePlanApprovalRequested(domain.PlanApprovalRequestedEvent{
			PlanContent:  "# Plan\n- step 1",
			PlanPath:     ".infer/plans/2026-06-28-090000-plan.md",
			ResponseChan: responseChan,
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.SetupPlanApprovalUIStateCallCount() != 1 {
			t.Fatalf("expected SetupPlanApprovalUIState once, got %d", state.SetupPlanApprovalUIStateCallCount())
		}
		gotContent, gotPath, gotChan := state.SetupPlanApprovalUIStateArgsForCall(0)
		if gotContent != "# Plan\n- step 1" {
			t.Errorf("unexpected plan content: %q", gotContent)
		}
		if gotPath != ".infer/plans/2026-06-28-090000-plan.md" {
			t.Errorf("expected plan path to be forwarded to state manager, got %q", gotPath)
		}
		if gotChan != responseChan {
			t.Errorf("expected response channel to be forwarded to state manager")
		}
	})
}

func TestService_HandlePlanApprovalResponse(t *testing.T) {
	t.Run("nil approval UI state returns nil cmd and restart=false without side effects", func(t *testing.T) {
		svc, _, state, _ := newCoordinator()
		state.GetPlanApprovalUIStateReturns(nil)

		cmd, restart := svc.HandlePlanApprovalResponse(domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalAccept,
		})

		if cmd != nil {
			t.Errorf("expected nil cmd when UI state is nil")
		}
		if restart {
			t.Errorf("expected restart=false when UI state is nil")
		}
		if state.ClearPlanApprovalUIStateCallCount() != 0 {
			t.Errorf("expected ClearPlanApprovalUIState NOT to be called when state was already nil")
		}
		if state.SetAgentModeCallCount() != 0 {
			t.Errorf("expected SetAgentMode NOT to be called when state was already nil")
		}
	})

	t.Run("Accept clears UI state, switches to standard mode, adds hidden continue, requests restart", func(t *testing.T) {
		svc, repo, state, _ := newCoordinator()
		state.GetPlanApprovalUIStateReturns(&domain.PlanApprovalUIState{PlanContent: "p"})

		cmd, restart := svc.HandlePlanApprovalResponse(domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalAccept,
		})

		if !restart {
			t.Errorf("Accept should request restart")
		}
		if cmd == nil {
			t.Errorf("expected non-nil cmd")
		}
		if state.ClearPlanApprovalUIStateCallCount() != 1 {
			t.Errorf("expected ClearPlanApprovalUIState once")
		}
		if state.SetAgentModeCallCount() != 1 {
			t.Errorf("expected SetAgentMode once")
		}
		if mode := state.SetAgentModeArgsForCall(0); mode != domain.AgentModeStandard {
			t.Errorf("expected AgentModeStandard, got %v", mode)
		}
		if state.EndChatSessionCallCount() != 0 {
			t.Errorf("EndChatSession should not be called on Accept")
		}
		if got := len(repo.GetMessages()); got != 1 {
			t.Errorf("expected 1 hidden continue message added, got %d", got)
		}
	})

	t.Run("AcceptAndAutoApprove switches to auto-accept mode and requests restart", func(t *testing.T) {
		svc, repo, state, _ := newCoordinator()
		state.GetPlanApprovalUIStateReturns(&domain.PlanApprovalUIState{PlanContent: "p"})

		_, restart := svc.HandlePlanApprovalResponse(domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalAcceptAndAutoApprove,
		})

		if !restart {
			t.Errorf("AcceptAndAutoApprove should request restart")
		}
		if mode := state.SetAgentModeArgsForCall(0); mode != domain.AgentModeAutoAccept {
			t.Errorf("expected AgentModeAutoAccept, got %v", mode)
		}
		if got := len(repo.GetMessages()); got != 1 {
			t.Errorf("expected 1 hidden continue message added, got %d", got)
		}
	})

	t.Run("Reject ends chat session, does not switch mode, does not request restart", func(t *testing.T) {
		svc, repo, state, _ := newCoordinator()
		state.GetPlanApprovalUIStateReturns(&domain.PlanApprovalUIState{PlanContent: "p"})

		_, restart := svc.HandlePlanApprovalResponse(domain.PlanApprovalResponseEvent{
			Action: domain.PlanApprovalReject,
		})

		if restart {
			t.Errorf("Reject should not request restart")
		}
		if state.SetAgentModeCallCount() != 0 {
			t.Errorf("Reject should not switch agent mode")
		}
		if state.EndChatSessionCallCount() != 1 {
			t.Errorf("expected EndChatSession on Reject, got %d calls", state.EndChatSessionCallCount())
		}
		if got := len(repo.GetMessages()); got != 0 {
			t.Errorf("expected no hidden continue message on Reject, got %d", got)
		}
	})
}

func TestService_HandleComputerUsePaused(t *testing.T) {
	t.Run("cancels request, marks paused, returns non-nil cmd", func(t *testing.T) {
		svc, _, state, agent := newCoordinator()

		cmd := svc.HandleComputerUsePaused(domain.ComputerUsePausedEvent{
			RequestID: "req-1",
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if agent.CancelRequestCallCount() != 1 {
			t.Errorf("expected CancelRequest once, got %d", agent.CancelRequestCallCount())
		}
		if got := agent.CancelRequestArgsForCall(0); got != "req-1" {
			t.Errorf("expected CancelRequest('req-1'), got %q", got)
		}
		if state.SetComputerUsePausedCallCount() != 1 {
			t.Errorf("expected SetComputerUsePaused once")
		}
		paused, reqID := state.SetComputerUsePausedArgsForCall(0)
		if !paused || reqID != "req-1" {
			t.Errorf("expected SetComputerUsePaused(true, 'req-1'), got (%v, %q)", paused, reqID)
		}
	})

	t.Run("still marks paused even if agent cancel returns an error", func(t *testing.T) {
		svc, _, state, agent := newCoordinator()
		agent.CancelRequestReturns(errors.New("no such request"))

		cmd := svc.HandleComputerUsePaused(domain.ComputerUsePausedEvent{
			RequestID: "stale",
		})

		if cmd == nil {
			t.Fatalf("expected non-nil cmd even on cancel error")
		}
		if state.SetComputerUsePausedCallCount() != 1 {
			t.Errorf("paused state should still be set even when cancel fails")
		}
	})
}

func TestService_HandleComputerUseResumed(t *testing.T) {
	t.Run("clears pause state, adds hidden continue, requests restart", func(t *testing.T) {
		svc, repo, state, _ := newCoordinator()

		cmd, restart := svc.HandleComputerUseResumed(domain.ComputerUseResumedEvent{})

		if !restart {
			t.Errorf("expected restart=true")
		}
		if cmd == nil {
			t.Fatalf("expected non-nil cmd")
		}
		if state.ClearComputerUsePauseStateCallCount() != 1 {
			t.Errorf("expected ClearComputerUsePauseState once")
		}
		if got := len(repo.GetMessages()); got != 1 {
			t.Errorf("expected 1 hidden continue message added, got %d", got)
		}
	})
}
