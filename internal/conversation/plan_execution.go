package conversation

import (
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

// PlanExecutionPrompt tells the agent to resume and execute an approved plan.
// The plan runs in a fresh session, so the prompt points the agent back at the
// stored plan instead of keeping the planning conversation in context.
func PlanExecutionPrompt(planID string) string {
	if planID == "" {
		return "The plan has been approved. Please proceed with executing it step by step. " +
			"Start by taking the first action required to implement the plan."
	}
	return fmt.Sprintf(
		"The plan has been approved. A fresh session has been started. "+
			"The full approved plan is stored as %q - run `infer plans show %s` to recall it, "+
			"then execute it step by step, starting with the first action required to implement it.",
		planID, planID,
	)
}

// StartPlanExecution starts the session an approved plan runs in: a fresh
// conversation titled after the planning one, opened by a hidden message that
// points the agent at the stored plan. The planning conversation stays stored.
func StartPlanExecution(repo convdomain.ConversationRepository, planID string) error {
	title := fmt.Sprintf("Continued from %s", repo.GetCurrentConversationTitle())
	if err := repo.StartNewConversation(title); err != nil {
		return fmt.Errorf("starting the plan's session: %w", err)
	}
	return repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(PlanExecutionPrompt(planID))},
		Time:    time.Now(),
		Hidden:  true,
	})
}
