package approvalcoord

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// planRepoUpdater is the narrow interface the coordinator uses to mutate plan
// approval state on the conversation repo. Both *services.InMemoryConversation
// Repository and *services.PersistentConversationRepository satisfy it (the
// latter via embedding).
type planRepoUpdater interface {
	UpdatePlanStatus(action domain.PlanApprovalAction)
}

// Service owns the UI side of "pause the assistant turn pending external
// decision" events.
type Service struct {
	agentService     domain.AgentService
	conversationRepo domain.ConversationRepository
	stateManager     domain.StateManager
}

// Options bundles the dependencies needed to construct a Service.
type Options struct {
	AgentService     domain.AgentService
	ConversationRepo domain.ConversationRepository
	StateManager     domain.StateManager
}

// NewService creates a new approval coordinator.
func NewService(opts Options) *Service {
	return &Service{
		agentService:     opts.AgentService,
		conversationRepo: opts.ConversationRepo,
		stateManager:     opts.StateManager,
	}
}

// HandlePlanApprovalRequested sets up the plan-approval UI state and emits an
// info status. Always returns a cmd; no restart side-effect.
func (s *Service) HandlePlanApprovalRequested(msg domain.PlanApprovalRequestedEvent) tea.Cmd {
	logger.Info("approvalCoordinator.HandlePlanApprovalRequested called")

	s.stateManager.SetupPlanApprovalUIState(msg.PlanContent, msg.PlanPath, msg.ResponseChan)

	return tea.Batch(s.planApprovalRequestedCmds(msg.PlanContent)...)
}

func (s *Service) planApprovalRequestedCmds(_ string) []tea.Cmd {
	return []tea.Cmd{
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    "Plan ready - use arrow keys to select and Enter to confirm",
				Spinner:    false,
				StatusType: domain.StatusDefault,
			}
		},
	}
}

// HandleUserQuestionRequested sets up the AskUserQuestion form state. The form
// renders as a floating box over the chat (like tool approval), so the view
// stays ViewStateChat and keys are intercepted while the form state is set.
// Unlike plan approval this does NOT stop the agent loop: the tool's Execute is
// blocked on the response channel and resumes when the user submits or cancels.
func (s *Service) HandleUserQuestionRequested(msg domain.UserQuestionRequestedEvent) tea.Cmd {
	s.stateManager.SetupUserQuestionUIState(msg.Questions, msg.ResponseChan)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Please answer the question(s) - ↑/↓ move, space toggle, enter to continue, esc to cancel",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

// HandlePlanApprovalResponse processes the user's accept/reject decision on a
// plan and returns whatever cmds the orchestrator should run plus a restart
// flag (true → orchestrator should kick a new ChatCompletionRunner.Start()).
func (s *Service) HandlePlanApprovalResponse(msg domain.PlanApprovalResponseEvent) (tea.Cmd, bool) {
	logger.Info("approvalCoordinator.HandlePlanApprovalResponse called", "action", msg.Action)

	planApprovalState := s.stateManager.GetPlanApprovalUIState()
	if planApprovalState == nil {
		logger.Warn("approvalCoordinator.HandlePlanApprovalResponse: planApprovalState is nil, ignoring")
		return nil, false
	}

	logger.Info("clearing plan approval UI state to prevent re-entry")
	s.stateManager.ClearPlanApprovalUIState()

	s.updatePlanStatus(msg.Action)

	statusMessage, restart := s.applyPlanDecision(msg.Action)

	cmds := []tea.Cmd{
		func() tea.Msg {
			history := s.conversationRepo.GetMessages()
			return domain.UpdateHistoryEvent{
				History: history,
			}
		},
		func() tea.Msg {
			return domain.SetStatusEvent{
				Message:    statusMessage,
				Spinner:    msg.Action != domain.PlanApprovalReject,
				StatusType: domain.StatusDefault,
			}
		},
	}

	return tea.Batch(cmds...), restart
}

// applyPlanDecision performs the agent-mode + session-state side effects for
// each plan-approval action and returns the status string + restart flag.
func (s *Service) applyPlanDecision(action domain.PlanApprovalAction) (string, bool) {
	switch action {
	case domain.PlanApprovalAccept:
		logger.Info("switching to auto-accept mode for plan execution")
		s.stateManager.SetAgentMode(domain.AgentModeAutoAccept)
		logger.Info("adding hidden continue message to queue (auto-approve mode)")
		s.addHiddenContinueMessage()
		return "Plan accepted - Auto-Approve mode enabled, executing plan...", true
	case domain.PlanApprovalAcceptStandard:
		logger.Info("switching to standard agent mode for plan execution")
		s.stateManager.SetAgentMode(domain.AgentModeStandard)
		logger.Info("adding hidden continue message to queue")
		s.addHiddenContinueMessage()
		return "Plan accepted - executing plan (approving each step)...", true
	case domain.PlanApprovalReject:
		logger.Info("ending chat session due to plan rejection")
		s.stateManager.EndChatSession()
		return "Plan rejected - you can provide feedback or changes", false
	}
	return "", false
}

// updatePlanStatus mutates the most recent pending plan entry on the
// conversation repo. Falls back silently if the repo doesn't implement the
// concrete updater interface (e.g. tests with stub repos).
func (s *Service) updatePlanStatus(action domain.PlanApprovalAction) {
	updater, ok := s.conversationRepo.(planRepoUpdater)
	if !ok {
		return
	}
	logger.Info("updating plan status")
	updater.UpdatePlanStatus(action)
}

// HandleComputerUsePaused cancels the in-flight request and marks state as
// paused. No restart - the user will manually resume.
func (s *Service) HandleComputerUsePaused(msg domain.ComputerUsePausedEvent) tea.Cmd {
	logger.Info("computer use execution paused", "request_id", msg.RequestID)

	if err := s.agentService.CancelRequest(msg.RequestID); err != nil {
		logger.Error("failed to cancel request on pause", "error", err, "request_id", msg.RequestID)
	}

	s.stateManager.SetComputerUsePaused(true, msg.RequestID)

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Computer use paused by user",
			Spinner:    false,
			StatusType: domain.StatusDefault,
		}
	}
}

// HandleComputerUseResumed clears the pause state, injects a hidden "please
// continue" user message, and signals the orchestrator to restart streaming.
func (s *Service) HandleComputerUseResumed(_ domain.ComputerUseResumedEvent) (tea.Cmd, bool) {
	s.stateManager.ClearComputerUsePauseState()

	if err := s.addHiddenContinue("Please continue from where you left off."); err != nil {
		return func() tea.Msg {
			return domain.ShowErrorEvent{
				Error:  fmt.Sprintf("Failed to resume: %v", err),
				Sticky: false,
			}
		}, false
	}

	return func() tea.Msg {
		return domain.SetStatusEvent{
			Message:    "Resuming execution...",
			Spinner:    true,
			StatusType: domain.StatusDefault,
		}
	}, true
}

// addHiddenContinueMessage injects the plan-approval continuation prompt.
func (s *Service) addHiddenContinueMessage() {
	logger.Info("addHiddenContinueMessage called")
	const planContinuePrompt = "The plan has been approved. Please proceed with executing it step by step. Start by taking the first action required to implement the plan."

	if err := s.addHiddenContinue(planContinuePrompt); err != nil {
		logger.Error("failed to add continue message to conversation", "error", err)
		return
	}
	logger.Info("continue message added to conversation history")
}

// addHiddenContinue appends a hidden user message to the conversation repo.
func (s *Service) addHiddenContinue(content string) error {
	entry := domain.ConversationEntry{
		Message: sdk.Message{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(content),
		},
		Time:   time.Now(),
		Hidden: true,
	}
	return s.conversationRepo.AddMessage(entry)
}
