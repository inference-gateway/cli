package agent

import (
	"context"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/states"
)

// A nil rollover manager (compact disabled / non-persistent storage) must make
// the per-turn rollover check a no-op instead of panicking or rebuilding.
func TestMaybeRolloverSession_NilManagerIsNoop(t *testing.T) {
	s := &AgentServiceImpl{}
	conv := []sdk.Message{{Role: sdk.User, Content: sdk.NewMessageContent("task")}}
	agentCtx := &states.AgentContext{Ctx: context.Background(), Conversation: &conv}

	s.maybeRolloverSession(agentCtx, &agentdomain.AgentRequest{RequestID: "s1", Model: "m"})

	if len(conv) != 1 {
		t.Fatalf("conversation length = %d, want untouched (1)", len(conv))
	}
}
