package history_test

import (
	"context"
	"reflect"
	"testing"

	storage "github.com/inference-gateway/cli/internal/platform/storage"
	history "github.com/inference-gateway/cli/internal/presentation/tui/history"
	projects "github.com/inference-gateway/cli/internal/projects"
)

func TestGetAllHistoryReturnsOrderedDefensiveCopy(t *testing.T) {
	hm := history.NewMemoryOnlyStore(5)

	for _, entry := range []string{"first prompt", "second prompt"} {
		if err := hm.AddToHistory(entry); err != nil {
			t.Fatalf("AddToHistory(%q): %v", entry, err)
		}
	}

	want := []string{"first prompt", "second prompt"}
	if got := hm.GetAllHistory(); !reflect.DeepEqual(got, want) {
		t.Fatalf("GetAllHistory = %v, want %v", got, want)
	}

	got := hm.GetAllHistory()
	got[0] = "mutated"
	if hm.GetAllHistory()[0] != "first prompt" {
		t.Error("GetAllHistory should return a defensive copy")
	}
}

func TestStoreBackedHistoryLoadsAndPersistsTheProject(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStorage()
	if err := store.AppendHistory(ctx, projects.Path(), "existing command"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.AppendHistory(ctx, "/elsewhere", "other project"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	hm := history.NewHistoryManagerWithProvider(5, history.NewStoreShellHistory(store))
	if got := hm.GetAllHistory(); !reflect.DeepEqual(got, []string{"existing command"}) {
		t.Fatalf("loaded history = %v, want the project's entry only", got)
	}

	for _, entry := range []string{"new command", "new command", "  "} {
		if err := hm.AddToHistory(entry); err != nil {
			t.Fatalf("AddToHistory(%q): %v", entry, err)
		}
	}
	if hm.NavigateUp("draft") != "new command" {
		t.Error("NavigateUp should return the newest command")
	}
	if hm.NavigateDown("") != "draft" {
		t.Error("NavigateDown past the newest entry should restore the draft")
	}

	persisted, err := store.LoadHistory(ctx, projects.Path(), 0)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if want := []string{"existing command", "new command"}; !reflect.DeepEqual(persisted, want) {
		t.Fatalf("persisted = %v, want %v", persisted, want)
	}
}
