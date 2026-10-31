package teleport

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
		name          string
		owner         string
		leftOn        string
		deleteErr     error
		wantDeletes   int
		wantWorkspace string
		wantError     string
		wantLease     string
		wantLeftOn    string
	}{
		{name: "remote owner", owner: "k8s", wantDeletes: 1, wantWorkspace: "k8s"},
		{name: "remote delete fails", owner: "k8s", deleteErr: errors.New("forbidden"), wantDeletes: 1, wantError: "forbidden", wantLease: "k8s"},
		{name: "owned here", owner: "laptop", wantLease: "laptop"},
		{name: "nobody owns it"},
		{name: "unknown owner", owner: "ghost", wantError: "not registered", wantLease: "ghost"},
		{name: "came home from k8s", owner: "laptop", leftOn: "k8s", wantDeletes: 1, wantWorkspace: "k8s", wantLease: "laptop"},
		{name: "came home, nobody owns it", leftOn: "k8s", wantDeletes: 1, wantWorkspace: "k8s"},
		{name: "came home, delete fails", owner: "laptop", leftOn: "k8s", deleteErr: errors.New("forbidden"), wantDeletes: 1, wantError: "forbidden", wantLease: "laptop", wantLeftOn: "k8s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k8s := fakeWorkspace("k8s", agentdomain.WorkspaceKindKubernetes)
			k8s.DeleteReturns(tt.deleteErr)
			s, store := newService(t, &launcher{}, k8s)
			s.cfg.StateDir = t.TempDir()
			if tt.leftOn != "" {
				s.rememberRuntime("conv-1", tt.leftOn)
			}
			if tt.owner != "" {
				_, _, _ = store.ClaimLease(context.Background(), agentdomain.Lease{ConversationID: "conv-1", Workspace: tt.owner, Until: time.Now().Add(time.Hour)})
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
			if stopped.Type != frameThreadStopped || stopped.ID != "conv-1" || stopped.ProjectDir != "/proj" || stopped.Workspace != tt.wantWorkspace {
				t.Fatalf("thread_stopped = %+v", stopped)
			}
			if !strings.Contains(stopped.Error, tt.wantError) || (tt.wantError == "") != (stopped.Error == "") {
				t.Fatalf("thread_stopped error = %q, want %q", stopped.Error, tt.wantError)
			}
			lease, found, _ := store.GetLease(context.Background(), "conv-1")
			if found != (tt.wantLease != "") || lease.Workspace != tt.wantLease {
				t.Fatalf("lease = %+v, %v, want owner %q", lease, found, tt.wantLease)
			}
			if left := s.runtimeLeftOn("conv-1"); left != tt.wantLeftOn {
				t.Fatalf("runtime left on %q, want %q", left, tt.wantLeftOn)
			}
		})
	}
}
