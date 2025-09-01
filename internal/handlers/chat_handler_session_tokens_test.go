package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/shortcuts"
	"github.com/inference-gateway/sdk"
)

func TestFormatMetricsWithSessionTokens(t *testing.T) {
	conversationRepo := services.NewInMemoryConversationRepository(nil)
	shortcutRegistry := shortcuts.NewRegistry()

	handler := NewChatHandler(
		nil, // agentService
		conversationRepo,
		nil, // modelService
		nil, // configService
		nil, // toolService
		nil, // fileService
		shortcutRegistry,
	)

	err := conversationRepo.AddTokenUsage(100, 50, 150)
	if err != nil {
		t.Fatalf("Failed to add token usage: %v", err)
	}

	metrics := &domain.ChatMetrics{
		Duration: 1 * time.Second,
		Usage: &sdk.CompletionUsage{
			PromptTokens:     25,
			CompletionTokens: 15,
			TotalTokens:      40,
		},
	}

	result := handler.FormatMetrics(metrics)

	if !strings.Contains(result, "Input: 25 tokens") {
		t.Errorf("Expected current input tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Output: 15 tokens") {
		t.Errorf("Expected current output tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Total: 40 tokens") {
		t.Errorf("Expected current total tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Session Input: 100 tokens") {
		t.Errorf("Expected session input tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Session Output: 50 tokens") {
		t.Errorf("Expected session output tokens in result, got: %s", result)
	}
}
