package transfer

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sessionsmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func TestStopSessionDeletesTheRemoteRuntimeAndAnswers(t *testing.T) {
	tests := []struct {
		name            string
		owner           string
		deleteErr       error
		wantDeletes     int
		wantEnvironment string
		wantError       string
		wantLease       string
	}{
		{"remote owner", "k8s", nil, 1, "k8s", "", ""},
		{"remote delete fails", "k8s", errors.New("forbidden"), 1, "", "forbidden", "k8s"},
		{"owned here", "laptop", nil, 0, "", "", "laptop"},
		{"nobody owns it", "", nil, 0, "", "", ""},
		{"unknown owner", "ghost", nil, 0, "", "not registered", "ghost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := fakeEnvironment("k8s", agentdomain.EnvironmentKindKubernetes)
			k8s.DeleteReturns(tt.deleteErr)
			s, store := newService(t, &launcher{}, k8s)
			if tt.owner != "" {
				_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Environment: tt.owner, Until: time.Now().Add(time.Hour)})
			}
			client := &sessionsmocks.FakeClient{}

			s.StopSession(t.Context(), client, testKey)

			if k8s.DeleteCallCount() != tt.wantDeletes {
				t.Fatalf("Delete called %d times, want %d", k8s.DeleteCallCount(), tt.wantDeletes)
			}
			if client.DeliverCallCount() != 1 {
				t.Fatalf("the client got %d frames, want one thread_stopped", client.DeliverCallCount())
			}
			var stopped threadStoppedFrame
			if err := json.Unmarshal(client.DeliverArgsForCall(0), &stopped); err != nil {
				t.Fatal(err)
			}
			if stopped.Type != frameThreadStopped || stopped.ID != "conv-1" || stopped.ProjectDir != "/proj" || stopped.Environment != tt.wantEnvironment {
				t.Fatalf("thread_stopped = %+v", stopped)
			}
			if !strings.Contains(stopped.Error, tt.wantError) || (tt.wantError == "") != (stopped.Error == "") {
				t.Fatalf("thread_stopped error = %q, want %q", stopped.Error, tt.wantError)
			}
			lease, found, _ := store.GetLease(context.Background(), "conv-1")
			if found != (tt.wantLease != "") || lease.Environment != tt.wantLease {
				t.Fatalf("lease = %+v, %v, want owner %q", lease, found, tt.wantLease)
			}
		})
	}
}
