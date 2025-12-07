package handlers

import (
	"strings"
	"testing"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/shortcuts"
	"github.com/inference-gateway/sdk"
)

func TestFormatMetricsWithoutSessionTokens(t *testing.T) {
	conversationRepo := services.NewInMemoryConversationRepository(nil)
	shortcutRegistry := shortcuts.NewRegistry()

	messageQueue := services.NewMessageQueueService()

	handler := NewChatHandler(
		nil, // agentService
		conversationRepo,
		nil, // conversationOptimizer
		nil, // modelService
		nil, // configService
		nil, // toolService
		nil, // fileService
		nil, // imageService
		shortcutRegistry,
		nil, // stateManager
		messageQueue,
		nil, // taskRetentionService
		nil, // backgroundTaskService
		nil, // backgroundShellService
		nil, // agentManager
		config.DefaultConfig(),
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

	// Verify current request tokens are displayed
	if !strings.Contains(result, "Input: 25 tokens") {
		t.Errorf("Expected current input tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Output: 15 tokens") {
		t.Errorf("Expected current output tokens in result, got: %s", result)
	}

	if !strings.Contains(result, "Total: 40 tokens") {
		t.Errorf("Expected current total tokens in result, got: %s", result)
	}

	// Session tokens should NOT be displayed in the status bar anymore
	// They are now available via the /context shortcut instead
	if strings.Contains(result, "Session Input") {
		t.Errorf("Session Input tokens should not be in status bar, got: %s", result)
	}

	if strings.Contains(result, "Session Output") {
		t.Errorf("Session Output tokens should not be in status bar, got: %s", result)
	}
}
