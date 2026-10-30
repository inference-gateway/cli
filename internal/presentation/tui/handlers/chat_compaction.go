package handlers

import (
	"time"

	tea "charm.land/bubbletea/v2"

	sdk "github.com/inference-gateway/sdk"

	conversation "github.com/inference-gateway/cli/internal/conversation"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// compactionTimeout bounds the LLM summarization call so a wedged gateway can't
// hang the UI thread. Shared by /compact and the post-plan-approval flow.
const compactionTimeout = 70 * time.Second

// optimizeWithTimeout force-compacts messages under a hard timeout. It returns
// (optimized, true) on success and (nil, false) if the summarization timed out.
func (h *ChatHandler) optimizeWithTimeout(messages []sdk.Message, model string) ([]sdk.Message, bool) {
	optimizedChan := make(chan []sdk.Message, 1)
	go func() {
		optimizedChan <- h.conversationOptimizer.OptimizeMessages(messages, model, true)
	}()

	select {
	case optimized := <-optimizedChan:
		return optimized, true
	case <-time.After(compactionTimeout):
		logger.Error("optimization timed out", "timeout", compactionTimeout)
		return nil, false
	}
}

// newSessionAfterPlanApproval starts the fresh conversation an approved plan
// runs in (see conversation.StartPlanExecution). The old conversation is
// preserved in storage. Errors are logged but never block execution (fail open).
func (h *ChatHandler) newSessionAfterPlanApproval(planID string) {
	if err := conversation.StartPlanExecution(h.conversationRepo, planID); err != nil {
		logger.Error("failed to start new session after plan approval", "error", err)
		return
	}
	logger.Info("started new session after plan approval")
}

// newSessionThenExecutePlanCmd starts a fresh empty session (like /new) and then
// resumes the agent to execute the approved plan. The agent re-reads the stored
// plan (planID), so the plan itself need not survive in context. The generic
// continue message the coordinator queued is wiped by the new session, so we
// re-add a plan-id-aware one.
func (h *ChatHandler) newSessionThenExecutePlanCmd(planID string) tea.Cmd {
	session := h.stateManager.GetChatSession()
	return func() tea.Msg {
		h.newSessionAfterPlanApproval(planID)

		return tea.Batch(
			func() tea.Msg {
				return tui.UpdateHistoryEvent{History: h.conversationRepo.GetMessages()}
			},
			func() tea.Msg { return tui.ChatCompletionRequestedEvent{Session: session} },
		)()
	}
}
