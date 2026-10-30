package conversation

import (
	"strings"
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

func TestPlanExecutionPrompt(t *testing.T) {
	tests := []struct {
		name     string
		planID   string
		contains []string
		absent   string
	}{
		{name: "points at the stored plan", planID: "2026-06-28-090000-plan", contains: []string{"infer plans show 2026-06-28-090000-plan", "fresh session"}},
		{name: "generic without an id", planID: "", contains: []string{"proceed"}, absent: "infer plans show"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PlanExecutionPrompt(tt.planID)
			for _, want := range tt.contains {
				if !strings.Contains(got, want) {
					t.Errorf("prompt %q lacks %q", got, want)
				}
			}
			if tt.absent != "" && strings.Contains(got, tt.absent) {
				t.Errorf("prompt %q contains %q", got, tt.absent)
			}
		})
	}
}

func TestStartPlanExecutionOpensAFreshSessionWithAHiddenPrompt(t *testing.T) {
	repo := &convmocks.FakeConversationRepository{}
	repo.GetCurrentConversationTitleReturns("Original")

	if err := StartPlanExecution(repo, "plan-1"); err != nil {
		t.Fatalf("StartPlanExecution: %v", err)
	}
	if repo.StartNewConversationCallCount() != 1 || repo.StartNewConversationArgsForCall(0) != "Continued from Original" {
		t.Fatalf("new conversation = %d calls, want one titled after the planning one", repo.StartNewConversationCallCount())
	}
	if repo.AddMessageCallCount() != 1 || !repo.AddMessageArgsForCall(0).Hidden {
		t.Fatalf("messages added = %d, want one hidden plan prompt", repo.AddMessageCallCount())
	}
}
