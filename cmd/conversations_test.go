package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

func TestRenderConversationsJSON(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "12345678-1234-1234-1234-123456789abc",
			Title:        "Test Conversation 1",
			CreatedAt:    time.Now().Add(-2 * time.Hour),
			UpdatedAt:    time.Now().Add(-1 * time.Hour),
			MessageCount: 5,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  1000,
				TotalOutputTokens: 2000,
				RequestCount:      3,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.142,
			},
		},
		{
			ID:           "87654321-4321-4321-4321-cba987654321",
			Title:        "Test Conversation 2",
			CreatedAt:    time.Now().Add(-24 * time.Hour),
			UpdatedAt:    time.Now().Add(-12 * time.Hour),
			MessageCount: 10,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  5000,
				TotalOutputTokens: 8000,
				RequestCount:      7,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.387,
			},
		},
	}

	err := renderConversationsJSON(conversations)
	if err != nil {
		t.Fatalf("renderConversationsJSON() failed: %v", err)
	}
}

func TestRenderConversationsJSON_Structure(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "test-id-1",
			Title:        "Test 1",
			MessageCount: 5,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      2,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.05,
			},
		},
	}

	output := struct {
		Conversations []storage.ConversationSummary `json:"conversations"`
		Count         int                           `json:"count"`
	}{
		Conversations: conversations,
		Count:         len(conversations),
	}

	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"conversations"`) {
		t.Error("JSON output missing 'conversations' field")
	}
	if !strings.Contains(jsonStr, `"count"`) {
		t.Error("JSON output missing 'count' field")
	}
	if !strings.Contains(jsonStr, `"test-id-1"`) {
		t.Error("JSON output missing conversation ID")
	}
}

func TestRenderConversationsTable_Empty(t *testing.T) {
	conversations := []storage.ConversationSummary{}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with empty conversations failed: %v", err)
	}
}

func TestRenderConversationsTable_WithData(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "12345678-1234-1234-1234-123456789abc",
			Title:        "Test Conversation",
			MessageCount: 10,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  1500,
				TotalOutputTokens: 2500,
				RequestCount:      5,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.234,
			},
		},
	}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() failed: %v", err)
	}
}

func TestRenderConversationsTable_Pagination(t *testing.T) {
	conversations := make([]storage.ConversationSummary, 60)
	for i := 0; i < 60; i++ {
		conversations[i] = storage.ConversationSummary{
			ID:           "test-id",
			Title:        "Test",
			MessageCount: i + 1,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.01,
			},
		}
	}

	err := renderConversationsTable(conversations[:50], 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() page 1 failed: %v", err)
	}

	err = renderConversationsTable(conversations[50:], 50, 50)
	if err != nil {
		t.Fatalf("renderConversationsTable() page 2 failed: %v", err)
	}
}

func TestRenderConversationsTable_LongTitle(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "This is a very long conversation title that should be truncated to 25 characters",
			MessageCount: 5,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.05,
			},
		},
	}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with long title failed: %v", err)
	}
}

func TestRenderConversationsTable_ZeroCost(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "Zero Cost Test",
			MessageCount: 3,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  50,
				TotalOutputTokens: 100,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.0,
			},
		},
	}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with zero cost failed: %v", err)
	}
}

func TestRenderConversationsTable_VariousCosts(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "test-id-1",
			Title:        "Very small cost",
			MessageCount: 1,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  10,
				TotalOutputTokens: 20,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.0023,
			},
		},
		{
			ID:           "test-id-2",
			Title:        "Medium cost",
			MessageCount: 1,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 0.142,
			},
		},
		{
			ID:           "test-id-3",
			Title:        "Large cost",
			MessageCount: 1,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  1000,
				TotalOutputTokens: 2000,
				RequestCount:      1,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 5.47,
			},
		},
	}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with various costs failed: %v", err)
	}
}

func TestRenderConversationsTable_LargeNumbers(t *testing.T) {
	conversations := []storage.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "Large token counts",
			MessageCount: 100,
			TokenStats: domain.SessionTokenStats{
				TotalInputTokens:  1234567,
				TotalOutputTokens: 9876543,
				RequestCount:      500,
			},
			CostStats: domain.SessionCostStats{
				TotalCost: 123.45,
			},
		},
	}

	err := renderConversationsTable(conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with large numbers failed: %v", err)
	}
}

func TestRenderMarkdown(t *testing.T) {
	markdown := "# Test Header\n\nSome **bold** text."

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() returned error (may fail in test environment): %v", err)
	}
}

func TestRenderMarkdown_EmptyString(t *testing.T) {
	markdown := ""

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with empty string returned error: %v", err)
	}
}

func TestRenderMarkdown_Table(t *testing.T) {
	markdown := `| ID | Title | Count |
|-----|-------|-------|
| 123 | Test  | 5     |
`

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with table returned error: %v", err)
	}
}

// makeShowEntries returns a fixture with a mix of user/assistant/tool/hidden
// entries used to exercise the `conversations show` helpers.
func makeShowEntries() []domain.ConversationEntry {
	t0 := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	toolID := "call_x"
	return []domain.ConversationEntry{
		{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hello world")},
			Time:    t0,
		},
		{
			Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("hi there")},
			Model:   "gpt-4o",
			Time:    t0.Add(1 * time.Second),
		},
		{
			Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("tool result body"), ToolCallID: &toolID},
			Time:    t0.Add(2 * time.Second),
		},
		{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("system reminder injected")},
			Time:    t0.Add(3 * time.Second),
			Hidden:  true,
		},
	}
}

func TestFilterConversationEntries_HiddenExcludedByDefault(t *testing.T) {
	got := filterConversationEntries(makeShowEntries(), false)
	if len(got) != 3 {
		t.Fatalf("expected 3 visible entries, got %d", len(got))
	}
	for _, e := range got {
		if e.Hidden {
			t.Errorf("hidden entry leaked into default output")
		}
	}
}

func TestFilterConversationEntries_HiddenIncluded(t *testing.T) {
	entries := makeShowEntries()
	got := filterConversationEntries(entries, true)
	if len(got) != len(entries) {
		t.Fatalf("expected all %d entries with include-hidden, got %d", len(entries), len(got))
	}
}

func TestBuildConversationShowText_PlainAndHeaders(t *testing.T) {
	out := buildConversationShowText(makeShowEntries(), "session-123")

	wants := []string{
		"Conversation: session-123",
		"Entries: 4",
		"[user]",
		"[assistant]",
		"[model=gpt-4o]",
		"2026-05-29T10:00:00Z",
		"hello world",
		"hi there",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("text output missing %q\n---\n%s", w, out)
		}
	}
}

func TestBuildConversationShowText_HiddenTagAndToolCallID(t *testing.T) {
	entries := filterConversationEntries(makeShowEntries(), true)
	out := buildConversationShowText(entries, "s")

	if !strings.Contains(out, "[hidden]") {
		t.Errorf("expected [hidden] tag in output:\n%s", out)
	}
	if !strings.Contains(out, "[tool_call_id=call_x]") {
		t.Errorf("expected [tool_call_id=call_x] in output:\n%s", out)
	}
}

func TestBuildConversationShowText_Empty(t *testing.T) {
	out := buildConversationShowText(nil, "s")
	if !strings.Contains(out, "Entries: 0") {
		t.Errorf("expected 'Entries: 0' for empty conversation:\n%s", out)
	}
	if !strings.Contains(out, "(no entries)") {
		t.Errorf("expected '(no entries)' for empty conversation:\n%s", out)
	}
}

func TestBuildConversationShowJSON_OneObjectPerLine(t *testing.T) {
	entries := makeShowEntries()
	out, err := buildConversationShowJSON(entries)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != len(entries) {
		t.Fatalf("expected %d JSON lines, got %d", len(entries), len(lines))
	}

	decoded := make([]conversationShowEntry, 0, len(lines))
	for i, line := range lines {
		var e conversationShowEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("line %d not valid JSON object: %v (%q)", i, err, line)
		}
		decoded = append(decoded, e)
	}

	if decoded[0].Role != "user" || decoded[0].Content != "hello world" || decoded[0].Time != "2026-05-29T10:00:00Z" {
		t.Errorf("unexpected first entry: %+v", decoded[0])
	}
	if decoded[1].Model != "gpt-4o" {
		t.Errorf("expected assistant model gpt-4o, got %q", decoded[1].Model)
	}
	if decoded[2].ToolCallID != "call_x" {
		t.Errorf("expected tool_call_id call_x, got %q", decoded[2].ToolCallID)
	}
	if !decoded[3].Hidden {
		t.Errorf("expected fourth entry to be hidden")
	}
}

func TestBuildConversationShowJSON_OmitsEmptyOptionalFields(t *testing.T) {
	entry := domain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hey")},
		Time:    time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
	}
	out, err := buildConversationShowJSON([]domain.ConversationEntry{entry})
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	for _, omitted := range []string{"tool_call_id", `"hidden"`, `"model"`} {
		if strings.Contains(out, omitted) {
			t.Errorf("expected %s to be omitted from JSON: %s", omitted, out)
		}
	}
}

func TestBuildConversationShowJSON_Empty(t *testing.T) {
	out, err := buildConversationShowJSON(nil)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}
	if out != "" {
		t.Errorf("expected empty output for no entries, got %q", out)
	}
}

func TestToConversationShowEntry_Multimodal(t *testing.T) {
	got := toConversationShowEntry(domain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User},
		Images: []domain.ImageAttachment{
			{MimeType: "image/png", DisplayName: "screenshot.png"},
		},
		Time: time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
	})

	if !strings.Contains(got.Content, "[Image 1]") {
		t.Errorf("expected image-only content to render [Image 1], got %q", got.Content)
	}
}
