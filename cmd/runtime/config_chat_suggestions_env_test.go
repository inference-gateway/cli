package runtime

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
)

func TestChatSuggestionsEnvOverrides(t *testing.T) {
	cfg := config.DefaultConfig()

	t.Setenv("INFER_CHAT_SUGGESTIONS_ENABLED", "true")
	t.Setenv("INFER_CHAT_SUGGESTIONS_MODEL", "anthropic/claude-haiku")
	t.Setenv("INFER_CHAT_SUGGESTIONS_CONTEXT_MESSAGES", "6")
	t.Setenv("INFER_CHAT_SUGGESTIONS_TIMEOUT", "5")

	resolveViperEnvironmentVariables(newEnvViper(), cfg, "")

	assert.True(t, cfg.Chat.Suggestions.Enabled)
	assert.Equal(t, "anthropic/claude-haiku", cfg.Chat.Suggestions.Model)
	assert.Equal(t, 6, cfg.Chat.Suggestions.ContextMessages)
	assert.Equal(t, 5, cfg.Chat.Suggestions.Timeout)
	assert.Equal(t, 256, cfg.Chat.Suggestions.MaxTokens)
}
