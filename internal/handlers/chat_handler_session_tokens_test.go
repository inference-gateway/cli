package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/sdk"
)

func TestFormatMetricsWithSessionTokens(t *testing.T) {
	// Create a simple chat handler with minimal dependencies
	conversationRepo := services.NewInMemoryConversationRepository()

	// Create a minimal handler just for testing formatMetrics
	handler := &ChatHandler{
		conversationRepo: conversationRepo,
	}

	// Add some token usage to the session
	err := conversationRepo.AddTokenUsage(100, 50, 150)
	if err != nil {
		t.Fatalf("Failed to add token usage: %v", err)
	}

	// Create test metrics
	metrics := &domain.ChatMetrics{
		Duration: 1 * time.Second,
		Usage: &sdk.CompletionUsage{
			PromptTokens:     25,
			CompletionTokens: 15,
			TotalTokens:      40,
		},
	}

	// Test formatMetrics includes session totals
	result := handler.formatMetrics(metrics)

	// Should include current call metrics
	if !strings.Contains(result, "Input: 25 tokens") {
		t.Errorf("Expected current input tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Output: 15 tokens") {
		t.Errorf("Expected current output tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Total: 40 tokens") {
		t.Errorf("Expected current total tokens in result, got: %s", result)
	}

	// Should include session totals
	if !strings.Contains(result, "Session Input: 100 tokens") {
		t.Errorf("Expected session input tokens in result, got: %s", result)
	}
}
