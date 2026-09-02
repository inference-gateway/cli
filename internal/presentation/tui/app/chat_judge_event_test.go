package app

import (
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// A chat event the app does not classify as a domain event never reaches the
// chat handler, and the listener chain dies with it (tool card stuck "queued").
func TestIsDomainEvent_JudgeVerdict(t *testing.T) {
	if !isDomainEvent(agentdomain.JudgeVerdictChatEvent{}) {
		t.Fatal("JudgeVerdictChatEvent must be routed to the chat handler")
	}
}
