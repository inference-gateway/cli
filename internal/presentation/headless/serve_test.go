package headless

import (
	"testing"

	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

func TestLiveThreadIDFollowsTheConversationTheRepoHolds(t *testing.T) {
	tests := []struct {
		name    string
		current string
		want    string
	}{
		{name: "the session's own conversation", current: "", want: "s1"},
		{name: "after a rollover or an accepted plan", current: "rolled", want: "rolled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &convmocks.FakeConversationRepository{}
			repo.GetCurrentConversationIDReturns(tt.current)
			if got := liveThreadID(repo, "s1"); got != tt.want {
				t.Errorf("liveThreadID() = %q, want %q", got, tt.want)
			}
		})
	}
}
