package compute

import (
	"context"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
)

func TestCheckAllKeepsTheEntriesOrder(t *testing.T) {
	entries := []int{3, 1, 2}
	got := checkAll(t.Context(), entries, func(_ context.Context, n int) int {
		time.Sleep(time.Duration(n) * time.Millisecond)
		return n * 10
	})
	if len(got) != 3 || got[0] != 30 || got[1] != 10 || got[2] != 20 {
		t.Fatalf("checkAll() = %v, want the results in the entries' order", got)
	}
}

func TestOnlyNarrowsToTheNamedEntry(t *testing.T) {
	clusters := []config.ClusterEntry{{Name: "a"}, {Name: "b"}}
	nameOf := func(c config.ClusterEntry) string { return c.Name }
	if all, err := only(clusters, "", nameOf, "cluster"); err != nil || len(all) != 2 {
		t.Fatalf("only() without a name = %v, %v, want both", all, err)
	}
	if one, err := only(clusters, "b", nameOf, "cluster"); err != nil || len(one) != 1 || one[0].Name != "b" {
		t.Fatalf("only(b) = %v, %v", one, err)
	}
	if _, err := only(clusters, "ghost", nameOf, "cluster"); err == nil || !strings.Contains(err.Error(), `cluster "ghost" is not in compute.yaml`) {
		t.Fatalf("only(ghost) = %v", err)
	}
}

func TestUsedByNamesTheWorkspacesOnTheCompute(t *testing.T) {
	daemon := config.DaemonConfig{Workspaces: []config.WorkspaceEntry{{Name: "reviewer", Compute: "k3d"}, {Name: "releaser", Compute: "k3d"}, {Name: "other", Compute: "box"}}}
	if err := usedBy(daemon, "k3d"); err == nil || !strings.Contains(err.Error(), "workspace reviewer, releaser") {
		t.Fatalf("usedBy(k3d) = %v, want both workspaces named", err)
	}
	if err := usedBy(daemon, "unused"); err != nil {
		t.Fatalf("usedBy(unused) = %v", err)
	}
}
