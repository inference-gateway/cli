package conversations

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/inference-gateway/sdk"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
)

func TestRenderConversationsJSON(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "12345678-1234-1234-1234-123456789abc",
			Title:        "Test Conversation 1",
			CreatedAt:    time.Now().Add(-2 * time.Hour),
			UpdatedAt:    time.Now().Add(-1 * time.Hour),
			MessageCount: 5,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  1000,
				TotalOutputTokens: 2000,
				RequestCount:      3,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.142,
			},
		},
		{
			ID:           "87654321-4321-4321-4321-cba987654321",
			Title:        "Test Conversation 2",
			CreatedAt:    time.Now().Add(-24 * time.Hour),
			UpdatedAt:    time.Now().Add(-12 * time.Hour),
			MessageCount: 10,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  5000,
				TotalOutputTokens: 8000,
				RequestCount:      7,
			},
			CostStats: convdomain.SessionCostStats{
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
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "test-id-1",
			Title:        "Test 1",
			MessageCount: 5,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      2,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.05,
			},
		},
	}

	output := struct {
		Conversations []convdomain.ConversationSummary `json:"conversations"`
		Count         int                              `json:"count"`
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
	conversations := []convdomain.ConversationSummary{}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with empty conversations failed: %v", err)
	}
}

func TestRenderConversationsTable_WithData(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "12345678-1234-1234-1234-123456789abc",
			Title:        "Test Conversation",
			MessageCount: 10,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  1500,
				TotalOutputTokens: 2500,
				RequestCount:      5,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.234,
			},
		},
	}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() failed: %v", err)
	}
}

func TestRenderConversationsTable_Pagination(t *testing.T) {
	conversations := make([]convdomain.ConversationSummary, 60)
	for i := 0; i < 60; i++ {
		conversations[i] = convdomain.ConversationSummary{
			ID:           "test-id",
			Title:        "Test",
			MessageCount: i + 1,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.01,
			},
		}
	}

	err := renderConversationsTable(output.NewRenderer(), conversations[:50], 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() page 1 failed: %v", err)
	}

	err = renderConversationsTable(output.NewRenderer(), conversations[50:], 50, 50)
	if err != nil {
		t.Fatalf("renderConversationsTable() page 2 failed: %v", err)
	}
}

func TestRenderConversationsTable_LongTitle(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "This is a very long conversation title that should be truncated to 25 characters",
			MessageCount: 5,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.05,
			},
		},
	}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with long title failed: %v", err)
	}
}

func TestRenderConversationsTable_ZeroCost(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "Zero Cost Test",
			MessageCount: 3,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  50,
				TotalOutputTokens: 100,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.0,
			},
		},
	}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with zero cost failed: %v", err)
	}
}

func TestRenderConversationsTable_VariousCosts(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "test-id-1",
			Title:        "Very small cost",
			MessageCount: 1,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  10,
				TotalOutputTokens: 20,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.0023,
			},
		},
		{
			ID:           "test-id-2",
			Title:        "Medium cost",
			MessageCount: 1,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  100,
				TotalOutputTokens: 200,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 0.142,
			},
		},
		{
			ID:           "test-id-3",
			Title:        "Large cost",
			MessageCount: 1,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  1000,
				TotalOutputTokens: 2000,
				RequestCount:      1,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 5.47,
			},
		},
	}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with various costs failed: %v", err)
	}
}

func TestRenderConversationsTable_LargeNumbers(t *testing.T) {
	conversations := []convdomain.ConversationSummary{
		{
			ID:           "test-id",
			Title:        "Large token counts",
			MessageCount: 100,
			TokenStats: convdomain.SessionTokenStats{
				TotalInputTokens:  1234567,
				TotalOutputTokens: 9876543,
				RequestCount:      500,
			},
			CostStats: convdomain.SessionCostStats{
				TotalCost: 123.45,
			},
		},
	}

	err := renderConversationsTable(output.NewRenderer(), conversations, 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() with large numbers failed: %v", err)
	}
}

func TestRenderMarkdown(t *testing.T) {
	markdown := "# Test Header\n\nSome **bold** text."

	_, err := output.NewRenderer().RenderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() returned error (may fail in test environment): %v", err)
	}
}

func TestRenderMarkdown_EmptyString(t *testing.T) {
	markdown := ""

	_, err := output.NewRenderer().RenderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with empty string returned error: %v", err)
	}
}

func TestRenderMarkdown_Table(t *testing.T) {
	markdown := `| ID | Title | Count |
|-----|-------|-------|
| 123 | Test  | 5     |
`

	_, err := output.NewRenderer().RenderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with table returned error: %v", err)
	}
}

// makeShowEntries returns a fixture with a mix of user/assistant/tool/hidden
// entries used to exercise the `conversations show` helpers.
func makeShowEntries() []convdomain.ConversationEntry {
	t0 := time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC)
	toolID := "call_x"
	return []convdomain.ConversationEntry{
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

func TestGrepConversationEntries(t *testing.T) {
	entries := makeShowEntries()

	got := grepConversationEntries(entries, "tool")
	if len(got) != 1 || got[0].Message.Role != sdk.Tool {
		t.Fatalf("expected only the tool entry for 'tool', got %d entries", len(got))
	}

	got = grepConversationEntries(entries, "HELLO")
	if len(got) != 1 || string(got[0].Message.Role) != "user" {
		t.Fatalf("expected case-insensitive match on 'hello world', got %d entries", len(got))
	}

	got = grepConversationEntries(entries, "")
	if len(got) != len(entries) {
		t.Fatalf("empty pattern must keep every entry, got %d of %d", len(got), len(entries))
	}

	got = grepConversationEntries(entries, "no-such-term")
	if len(got) != 0 {
		t.Fatalf("expected no matches, got %d", len(got))
	}
}

func TestBuildConversationShowText_PlainAndHeaders(t *testing.T) {
	entries := makeShowEntries()
	out := buildConversationShowText(entries, "session-123", 1, len(entries))

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
	out := buildConversationShowText(entries, "s", 1, len(entries))

	if !strings.Contains(out, "[hidden]") {
		t.Errorf("expected [hidden] tag in output:\n%s", out)
	}
	if !strings.Contains(out, "[tool_call_id=call_x]") {
		t.Errorf("expected [tool_call_id=call_x] in output:\n%s", out)
	}
}

func TestBuildConversationShowText_Empty(t *testing.T) {
	out := buildConversationShowText(nil, "s", 1, 0)
	if !strings.Contains(out, "Entries: 0") {
		t.Errorf("expected 'Entries: 0' for empty conversation:\n%s", out)
	}
	if !strings.Contains(out, "(no entries)") {
		t.Errorf("expected '(no entries)' for empty conversation:\n%s", out)
	}
}

func TestBuildConversationShowJSON_MetadataAndEntries(t *testing.T) {
	entries := makeShowEntries()
	metadata := convdomain.ConversationMetadata{
		ID:              "sess-1",
		Title:           "hello",
		ParentSessionID: "parent-1",
		InvokedBy:       "agent",
		TokenStats:      convdomain.SessionTokenStats{TotalInputTokens: 120, TotalOutputTokens: 30, TotalCachedTokens: 100, LastInputTokens: 80},
		CostStats:       convdomain.SessionCostStats{TotalCost: 0.42},
	}
	out, err := buildConversationShowJSON(entries, metadata, false)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	var decodedOut showConversationOutput
	if err := json.Unmarshal([]byte(out), &decodedOut); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if decodedOut.Metadata.ID != "sess-1" || decodedOut.Metadata.ParentSessionID != "parent-1" || decodedOut.Metadata.InvokedBy != "agent" {
		t.Errorf("unexpected metadata: %+v", decodedOut.Metadata)
	}
	if decodedOut.Metadata.TokenStats != metadata.TokenStats || decodedOut.Metadata.TotalCost != 0.42 {
		t.Errorf("token stats not projected: %+v", decodedOut.Metadata)
	}
	decoded := decodedOut.Entries
	if len(decoded) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(decoded))
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
	entry := convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("hey")},
		Time:    time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
	}
	out, err := buildConversationShowJSON([]convdomain.ConversationEntry{entry}, convdomain.ConversationMetadata{}, false)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	var doc struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if len(doc.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(doc.Entries))
	}

	for _, omitted := range []string{"tool_call_id", "hidden", "model", "tool_execution", "reasoning_content", "truncated"} {
		if _, present := doc.Entries[0][omitted]; present {
			t.Errorf("expected %s to be omitted from the entry: %s", omitted, out)
		}
	}
}

// A tool entry's content is a render for humans and the LLM, so a consumer that
// rebuilds a transcript cannot recover success from it - "error" in a commit
// subject reads the same as a real failure. The structured result must survive
// the projection.
func TestBuildConversationShowJSON_CarriesStructuredToolResult(t *testing.T) {
	toolCallID := "call_x"
	entry := convdomain.ConversationEntry{
		Message: sdk.Message{
			Role:       sdk.Tool,
			Content:    sdk.NewMessageContent("Bash(command=git commit -m \"fix: handle error path\")\n\u251c\u2500\u2500 Duration: 49ms\n\u251c\u2500\u2500 Status: \u2713 Success"),
			ToolCallID: &toolCallID,
		},
		Time: time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
		ToolExecution: &agentdomain.ToolExecutionResult{
			ToolName:  "Bash",
			Arguments: map[string]any{"command": "git commit -m \"fix: handle error path\""},
			Success:   true,
		},
	}
	out, err := buildConversationShowJSON([]convdomain.ConversationEntry{entry}, convdomain.ConversationMetadata{}, false)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	var decoded showConversationOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(decoded.Entries))
	}
	got := decoded.Entries[0].ToolExecution
	if got == nil {
		t.Fatalf("tool_execution dropped from the projection: %s", out)
	}
	if !got.Success || got.ToolName != "Bash" {
		t.Errorf("tool_execution = %+v, want Bash/success", got)
	}
}

func TestBuildConversationShowJSON_CarriesReasoningContent(t *testing.T) {
	entry := convdomain.ConversationEntry{
		Message:          sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("answer")},
		Time:             time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
		ReasoningContent: "thinking it through",
	}
	out, err := buildConversationShowJSON([]convdomain.ConversationEntry{entry}, convdomain.ConversationMetadata{}, false)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	var decoded showConversationOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if decoded.Entries[0].ReasoningContent != "thinking it through" {
		t.Errorf("reasoning_content = %q, want it projected", decoded.Entries[0].ReasoningContent)
	}
}

func TestBuildConversationShowJSON_Empty(t *testing.T) {
	out, err := buildConversationShowJSON(nil, convdomain.ConversationMetadata{}, false)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}
	var decoded showConversationOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if len(decoded.Entries) != 0 {
		t.Errorf("expected no entries, got %d", len(decoded.Entries))
	}
	if decoded.Metadata.InvokedBy != "human" {
		t.Errorf("expected invoked_by to default to human, got %q", decoded.Metadata.InvokedBy)
	}
}

func TestToConversationShowEntry_Multimodal(t *testing.T) {
	got := toConversationShowEntry(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User},
		Images: []agentdomain.ImageAttachment{
			{MimeType: "image/png", DisplayName: "screenshot.png"},
		},
		Time: time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
	})

	if !strings.Contains(got.Content, "[Image 1]") {
		t.Errorf("expected image-only content to render [Image 1], got %q", got.Content)
	}
}

func TestConversationsListCmd_AllProjectsFlag(t *testing.T) {
	command, _, err := NewCommand(runtime.NewState(), output.NewRenderer()).Find([]string{"list"})
	if err != nil {
		t.Fatalf("find list command: %v", err)
	}
	if cmd := command.Flags().Lookup("all-projects"); cmd == nil {
		t.Error("expected list to register the --all-projects flag")
	}
}

func TestConversationsDeleteCmd_Definition(t *testing.T) {
	command, _, err := NewCommand(runtime.NewState(), output.NewRenderer()).Find([]string{"delete"})
	if err != nil {
		t.Fatalf("find delete command: %v", err)
	}
	if command.Use != "delete <session-id>" {
		t.Errorf("expected Use 'delete <session-id>', got %q", command.Use)
	}
	if command.Short != "Delete a saved conversation" {
		t.Errorf("expected Short 'Delete a saved conversation', got %q", command.Short)
	}
	if command.Args == nil {
		t.Error("expected Args to be set")
	}
	if command.RunE == nil {
		t.Error("expected RunE to be set")
	}
}

func TestWindowConversationEntries(t *testing.T) {
	entries := make([]convdomain.ConversationEntry, 13)
	for i := range entries {
		entries[i] = convdomain.ConversationEntry{
			Message: sdk.Message{Content: sdk.NewMessageContent(fmt.Sprintf("entry %d", i+1))},
		}
	}

	tests := []struct {
		name      string
		noTrunc   bool
		showRest  bool
		wantStart int
		wantCount int
	}{
		{"default caps at the first window", false, false, 1, defaultShowEntries},
		{"no-trunc keeps everything", true, false, 1, 13},
		{"append shows what the window skipped", false, true, defaultShowEntries + 1, 3},
	}
	for _, tt := range tests {
		got, start, total := windowConversationEntries(entries, tt.noTrunc, tt.showRest)
		if start != tt.wantStart || len(got) != tt.wantCount {
			t.Errorf("%s: start/count = %d/%d, want %d/%d", tt.name, start, len(got), tt.wantStart, tt.wantCount)
		}
		if total != 13 {
			t.Errorf("%s: total = %d, want the filtered count 13", tt.name, total)
		}
	}

	rest, _, _ := windowConversationEntries(entries, false, true)
	if text := formatting.ExtractTextFromContent(rest[0].Message.Content, rest[0].Images); text != "entry 11" {
		t.Errorf("append window starts at the wrong entry: got %q, want %q", text, "entry 11")
	}

	short := entries[:5]
	got, start, total := windowConversationEntries(short, false, true)
	if len(got) != 5 || start != 1 || total != 5 {
		t.Errorf("short session with append: start/count/total = %d/%d/%d, want 1/5/5", start, len(got), total)
	}
}

// showConversation pipelines hidden-filter, grep, then window. With --grep the
// window must therefore skip the first 10 of the grep RESULTS - not of the raw
// session - so a paged read of matches starts where the matches do.
func TestShowPipelineWindowsAfterGrep(t *testing.T) {
	raw := make([]convdomain.ConversationEntry, 25)
	for i := range raw {
		text := fmt.Sprintf("entry-%02d just filler text", i)
		if i%2 == 0 {
			text = fmt.Sprintf("entry-%02d mentions the marker", i)
		}
		raw[i] = convdomain.ConversationEntry{
			Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(text)},
		}
	}

	wantAll := []int{0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24}
	sessionIndexes := func(entries []convdomain.ConversationEntry) []int {
		indexes := make([]int, 0, len(entries))
		for _, e := range entries {
			text := formatting.ExtractTextFromContent(e.Message.Content, e.Images)
			label := strings.Fields(text)[0]
			i, err := strconv.Atoi(strings.TrimPrefix(label, "entry-"))
			if err != nil {
				t.Fatalf("unexpected entry text %q", text)
			}
			indexes = append(indexes, i)
		}
		return indexes
	}

	matches := grepConversationEntries(filterConversationEntries(raw, false), "marker")
	if len(matches) != 13 {
		t.Fatalf("expected 13 matches out of 25 entries, got %d", len(matches))
	}

	head, start, total := windowConversationEntries(matches, false, false)
	if start != 1 || total != 13 {
		t.Errorf("head window: start/total = %d/%d, want 1/13", start, total)
	}
	if got := sessionIndexes(head); !slices.Equal(got, wantAll[:10]) {
		t.Errorf("head window picked %v, want the first 10 matches %v", got, wantAll[:10])
	}

	rest, start, total := windowConversationEntries(matches, false, true)
	if start != defaultShowEntries+1 || total != 13 {
		t.Errorf("append window: start/total = %d/%d, want %d/13", start, total, defaultShowEntries+1)
	}
	if got := sessionIndexes(rest); !slices.Equal(got, wantAll[10:]) {
		t.Errorf("append window picked %v, want the last matches %v - append must skip the first 10 of the grep results, not the session", got, wantAll[10:])
	}

	everything, start, total := windowConversationEntries(matches, true, false)
	if start != 1 || total != 13 || len(everything) != 13 {
		t.Errorf("no-trunc: start/total/len = %d/%d/%d, want 1/13/13", start, total, len(everything))
	}
	if got := sessionIndexes(everything); !slices.Equal(got, wantAll) {
		t.Errorf("no-trunc kept %v, want all matches %v", got, wantAll)
	}

	single := grepConversationEntries(filterConversationEntries(raw, false), "entry-01")
	got, start, total := windowConversationEntries(single, false, true)
	if len(got) != 1 || start != 1 || total != 1 {
		t.Errorf("short result set with append: start/total/len = %d/%d/%d, want 1/1/1", start, total, len(got))
	}
}

func TestBuildConversationShowText_AppendWindowKeepsRealNumbers(t *testing.T) {
	tail := makeShowEntries()[2:]
	out := buildConversationShowText(tail, "s", 3, 4)

	if !strings.Contains(out, "#3 [tool]") {
		t.Errorf("expected the first append-window entry to keep its session number:\n%s", out)
	}
	if !strings.Contains(out, "Skipped the first 2 entries") {
		t.Errorf("expected the skipped-entries hint:\n%s", out)
	}
}

func TestBuildConversationShowText_HeadWindowHintsAtTheRest(t *testing.T) {
	out := buildConversationShowText(makeShowEntries(), "s", 1, 9)

	if !strings.Contains(out, "Showing the first 4 of 9 entries") {
		t.Errorf("expected the first-window hint:\n%s", out)
	}
}

func TestBuildConversationShowJSON_FlagsTruncation(t *testing.T) {
	out, err := buildConversationShowJSON(makeShowEntries()[:2], convdomain.ConversationMetadata{}, true)
	if err != nil {
		t.Fatalf("buildConversationShowJSON() failed: %v", err)
	}

	var decoded showConversationOutput
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("output not valid JSON: %v (%q)", err, out)
	}
	if !decoded.Truncated {
		t.Errorf("expected truncated to be flagged: %s", out)
	}
}
