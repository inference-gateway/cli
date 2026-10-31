package daemonthread

import (
	"testing"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

func TestStorageNamesWhereEachListedSessionRuns(t *testing.T) {
	d := startDaemon(t, 0)
	leases := storage.NewMemoryStorage()
	_, _, _ = leases.ClaimLease(t.Context(), agentdomain.Lease{ConversationID: "away", Workspace: "k8s-dev", Until: time.Now().Add(time.Hour)})
	thread := New(d.config(), Options{ProjectDir: "/proj"}, leases)
	t.Cleanup(thread.Close)
	if err := thread.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	d.answer("list_conversations", `{"type":"conversations","conversations":[{"id":"away"},{"id":"here"}]}`)

	listed, err := NewStorage(thread).ListConversations(t.Context(), "", 50, 0)

	if err != nil || len(listed) != 2 || listed[0].Workspace != "k8s-dev" || listed[1].Workspace != "laptop" {
		t.Errorf("ListConversations = %+v, %v, want away on k8s-dev and here on laptop", listed, err)
	}
}
