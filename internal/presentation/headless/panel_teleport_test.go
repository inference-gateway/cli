package headless

import (
	"context"
	"testing"

	sdk "github.com/inference-gateway/sdk"

	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
)

func exportEntries() []convdomain.ConversationEntry {
	return []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hello")}},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("hi")}},
		{Message: sdk.Message{Role: sdk.System, Content: sdk.NewMessageContent("reminder")}, Hidden: true},
	}
}

func TestPanelExportsAStoredConversation(t *testing.T) {
	store := storage.NewMemoryStorage()
	metadata := convdomain.ConversationMetadata{ID: "conv-1", Title: "Greeting"}
	if err := store.SaveConversation(context.Background(), "conv-1", exportEntries(), metadata); err != nil {
		t.Fatal(err)
	}
	deps := panelDeps()
	deps.Storage = store
	p, sink := startPanel(deps)

	if !handle(t, p, map[string]any{"type": "export_conversation", "id": "conv-1"}) {
		t.Fatal("the panel did not consume export_conversation")
	}
	frame := readFrame(t, sink, "conversation_export")
	if frame["id"] != "conv-1" || frame["error"] != nil {
		t.Fatalf("export = %v", frame)
	}
	if got := frame["message_count"]; got != float64(2) {
		t.Fatalf("message_count = %v, want 2 visible messages", got)
	}
	if len(frame["entries"].([]any)) != 3 {
		t.Fatalf("export must carry every stored entry, hidden ones included: %v", frame["entries"])
	}
	if frame["digest"] == "" || frame["digest"] == nil {
		t.Fatal("export carries no digest")
	}
}

func TestPanelImportsAConversationIntoTheProject(t *testing.T) {
	store := storage.NewMemoryStorage()
	deps := panelDeps()
	deps.Storage = store
	p, sink := startPanel(deps)

	handle(t, p, map[string]any{
		"type": "import_conversation", "id": "conv-9",
		"entries":  exportEntries(),
		"metadata": convdomain.ConversationMetadata{ID: "conv-9", Title: "Moved", Project: "/elsewhere"},
	})
	imported := readFrame(t, sink, "conversation_imported")
	if imported["id"] != "conv-9" || imported["error"] != nil || imported["message_count"] != float64(2) {
		t.Fatalf("imported = %v", imported)
	}

	entries, metadata, err := store.LoadConversation(context.Background(), "conv-9")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || metadata.Title != "Moved" {
		t.Fatalf("stored %d entries with title %q", len(entries), metadata.Title)
	}
	if metadata.Project == "/elsewhere" {
		t.Fatal("the import kept the source project path, so list_conversations would not see it here")
	}

	handle(t, p, map[string]any{"type": "export_conversation", "id": "conv-9"})
	exported := readFrame(t, sink, "conversation_export")
	if exported["digest"] != imported["digest"] {
		t.Fatalf("export digest %v differs from the import digest %v", exported["digest"], imported["digest"])
	}
}

func TestPanelTeleportFramesReportMissingStorage(t *testing.T) {
	p, sink := startPanel(panelDeps())
	handle(t, p, map[string]any{"type": "export_conversation", "id": "conv-1"})
	if frame := readFrame(t, sink, "conversation_export"); frame["error"] == nil {
		t.Fatalf("export without storage reported no error: %v", frame)
	}
	handle(t, p, map[string]any{"type": "import_conversation", "id": "conv-1", "entries": []any{}})
	if frame := readFrame(t, sink, "conversation_imported"); frame["error"] == nil {
		t.Fatalf("import without storage reported no error: %v", frame)
	}
}
