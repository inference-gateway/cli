package infrastructure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := utils.RunGit(context.Background(), dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// podWork clones the checkout the way a transfer restores it, lets the
// session commit, edit and add a file there, and packs the result.
func podWork(t *testing.T, laptop string) agentdomain.Workspace {
	t.Helper()
	sent, err := PackWorkspace(t.Context(), laptop, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	pod := filepath.Join(t.TempDir(), "pod")
	if err := UnpackWorkspace(t.Context(), sent, pod, nil); err != nil {
		t.Fatal(err)
	}
	git(t, pod, "config", "user.email", "pod@example.com")
	git(t, pod, "config", "user.name", "Pod")
	git(t, pod, "config", "commit.gpgsign", "false")
	write(t, pod, "feature.go", "package feature\n")
	git(t, pod, "add", "feature.go")
	git(t, pod, "commit", "--quiet", "-m", "feature")
	write(t, pod, "README.md", "hello from the pod\n")
	write(t, pod, "notes.txt", "remember\n")
	back, err := PackWorkspace(t.Context(), pod, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func TestLandWorkspaceContinuesTheCheckout(t *testing.T) {
	laptop := gitRepo(t)
	ws := podWork(t, laptop)

	landing, err := LandWorkspace(t.Context(), ws, laptop, nil)
	if err != nil {
		t.Fatalf("LandWorkspace: %v", err)
	}
	if err := landing.Keep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := git(t, laptop, "log", "-1", "--format=%s"); got != "feature" {
		t.Errorf("HEAD is %q, want the pod's commit", got)
	}
	if got := git(t, laptop, "symbolic-ref", "--short", "HEAD"); got != "main" {
		t.Errorf("the checkout is on %q, want main", got)
	}
	if read(t, laptop, "README.md") != "hello from the pod\n" || read(t, laptop, "notes.txt") != "remember\n" {
		t.Error("the pod's uncommitted edit and untracked file did not land")
	}
}

func TestLandWorkspaceUndoRestoresADirtyCheckout(t *testing.T) {
	laptop := gitRepo(t)
	write(t, laptop, "README.md", "work in progress\n")
	write(t, laptop, "draft.txt", "draft\n")
	ws := podWork(t, laptop)
	before, _, err := CheckoutState(t.Context(), laptop, nil)
	if err != nil {
		t.Fatal(err)
	}

	landing, err := LandWorkspace(t.Context(), ws, laptop, nil)
	if err != nil {
		t.Fatalf("LandWorkspace: %v", err)
	}
	if err := landing.Undo(t.Context()); err != nil {
		t.Fatalf("Undo: %v", err)
	}

	after, _, err := CheckoutState(t.Context(), laptop, nil)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Error("the undone landing left the checkout different from before")
	}
	if _, err := os.Stat(filepath.Join(laptop, "notes.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the undone landing left the pod's untracked file behind")
	}
	if stashes := git(t, laptop, "stash", "list"); stashes != "" {
		t.Errorf("the undo left stashes %q", stashes)
	}
}

func TestLandWorkspaceRefusesADivergedCheckout(t *testing.T) {
	laptop := gitRepo(t)
	ws := podWork(t, laptop)
	write(t, laptop, "local.txt", "local\n")
	git(t, laptop, "add", "local.txt")
	git(t, laptop, "commit", "--quiet", "-m", "local")
	write(t, laptop, "README.md", "edited after the move\n")
	head := git(t, laptop, "rev-parse", "HEAD")

	if _, err := LandWorkspace(t.Context(), ws, laptop, nil); !errors.Is(err, ErrCheckoutDiverged) {
		t.Fatalf("LandWorkspace = %v, want ErrCheckoutDiverged", err)
	}

	if git(t, laptop, "rev-parse", "HEAD") != head || read(t, laptop, "README.md") != "edited after the move\n" {
		t.Error("the refused landing changed the checkout")
	}
	if stashes := git(t, laptop, "stash", "list"); stashes != "" {
		t.Errorf("the refused landing left stashes %q", stashes)
	}
}

func TestLandWorkspaceLeavesExcludedUntrackedFilesAlone(t *testing.T) {
	laptop := gitRepo(t)
	write(t, laptop, ".env", "SECRET=1\n")
	write(t, laptop, "draft.txt", "draft\n")
	excluded := func(abs string) bool { return filepath.Base(abs) == ".env" }
	sent, err := PackWorkspace(t.Context(), laptop, nil, excluded)
	if err != nil {
		t.Fatal(err)
	}
	pod := filepath.Join(t.TempDir(), "pod")
	if err := UnpackWorkspace(t.Context(), sent, pod, nil); err != nil {
		t.Fatal(err)
	}
	write(t, pod, "draft.txt", "draft, finished\n")
	ws, err := PackWorkspace(t.Context(), pod, nil, excluded)
	if err != nil {
		t.Fatal(err)
	}

	landing, err := LandWorkspace(t.Context(), ws, laptop, excluded)
	if err != nil {
		t.Fatalf("LandWorkspace: %v", err)
	}
	if err := landing.Keep(t.Context()); err != nil {
		t.Fatal(err)
	}

	if read(t, laptop, ".env") != "SECRET=1\n" {
		t.Error("the landing touched the excluded .env")
	}
	if read(t, laptop, "draft.txt") != "draft, finished\n" {
		t.Error("the pod's version of draft.txt did not land")
	}
	if stashes := git(t, laptop, "stash", "list"); stashes != "" {
		t.Errorf("Keep left stashes %q", stashes)
	}
}

func TestCheckoutStateFollowsTheTree(t *testing.T) {
	dir := gitRepo(t)
	clean, isClean, err := CheckoutState(t.Context(), dir, nil)
	if err != nil || !isClean {
		t.Fatalf("CheckoutState = %q, %v, %v, want a clean checkout", clean, isClean, err)
	}
	write(t, dir, "notes.txt", "a\n")
	first, isClean, _ := CheckoutState(t.Context(), dir, nil)
	again, _, _ := CheckoutState(t.Context(), dir, nil)
	write(t, dir, "notes.txt", "b\n")
	changed, _, _ := CheckoutState(t.Context(), dir, nil)

	if isClean || first == clean {
		t.Error("an untracked file left the checkout clean")
	}
	if first != again {
		t.Error("the fingerprint of an unchanged checkout moved")
	}
	if changed == first {
		t.Error("editing an untracked file kept the fingerprint")
	}
}

func TestCheckoutStateNamesAnUntrackedSymlinkWithoutFollowingIt(t *testing.T) {
	dir := gitRepo(t)
	before, _, err := CheckoutState(t.Context(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "outside")); err != nil {
		t.Fatal(err)
	}

	after, clean, err := CheckoutState(t.Context(), dir, nil)

	if err != nil || clean || after == before {
		t.Errorf("CheckoutState = %q, clean %v, %v, want the new symlink to change the fingerprint", after, clean, err)
	}
}
