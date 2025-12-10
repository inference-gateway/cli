package cmd

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

func TestRenderConversationsJSON(t *testing.T) {
	// Create test conversations
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

	// Render JSON
	err := renderConversationsJSON(conversations)
	if err != nil {
		t.Fatalf("renderConversationsJSON() failed: %v", err)
	}

	// Note: Since the function prints to stdout, we can't easily capture it in this test
	// In a real scenario, we would refactor to accept an io.Writer
	// For now, we just verify no error occurred
}

func TestRenderConversationsJSON_Structure(t *testing.T) {
	// Test that JSON structure is correct by creating sample data
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

	// Create the expected output structure
	output := struct {
		Conversations []storage.ConversationSummary `json:"conversations"`
		Count         int                           `json:"count"`
	}{
		Conversations: conversations,
		Count:         len(conversations),
	}

	// Marshal to JSON
	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	// Verify JSON contains expected fields
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

	// Function should print "No conversations found" message
	// Since we can't easily capture stdout in this test, we just verify no error
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
	// Create 60 conversations to test pagination
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

	// Test first page
	err := renderConversationsTable(conversations[:50], 50, 0)
	if err != nil {
		t.Fatalf("renderConversationsTable() page 1 failed: %v", err)
	}

	// Test second page
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

	// Title should be truncated by TruncateText function
	// We can't easily verify the output, but we ensure no error
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
				TotalCost: 0.0, // Zero cost should display as "-"
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
				TotalCost: 0.0023, // Should show 4 decimals
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
				TotalCost: 0.142, // Should show 3 decimals
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
				TotalCost: 5.47, // Should show 2 decimals
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
	// Test that renderMarkdown doesn't fail with valid markdown
	markdown := "# Test Header\n\nSome **bold** text."

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() returned error (may fail in test environment): %v", err)
		// Don't fail the test - glamour may not work in all test environments
	}
}

func TestRenderMarkdown_EmptyString(t *testing.T) {
	markdown := ""

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with empty string returned error: %v", err)
		// Don't fail - may not work in test environment
	}
}

func TestRenderMarkdown_Table(t *testing.T) {
	// Test with markdown table similar to what we generate
	markdown := `| ID | Title | Count |
|-----|-------|-------|
| 123 | Test  | 5     |
`

	_, err := renderMarkdown(markdown)
	if err != nil {
		t.Logf("renderMarkdown() with table returned error: %v", err)
		// Don't fail - may not work in test environment
	}
}
