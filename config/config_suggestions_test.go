package config

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
)

func TestEffectiveSuggestionsAppliesDefaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.False(t, cfg.Chat.Suggestions.Enabled)

	cfg.Chat.Suggestions = ChatSuggestionsConfig{Enabled: true}
	cfg.Agent.Model = "provider/model"

	got := cfg.EffectiveSuggestions()

	assert.Equal(t, "provider/model", got.Model)
	assert.Equal(t, 4, got.ContextMessages)
	assert.Equal(t, 256, got.MaxTokens)
	assert.Equal(t, 10, got.Timeout)
}

func TestEffectiveSuggestionsKeepsExplicitValues(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Chat.Suggestions = ChatSuggestionsConfig{
		Enabled:         true,
		Model:           "openai/gpt-4o-mini",
		ContextMessages: 6,
		MaxTokens:       32,
		Timeout:         5,
	}

	got := cfg.EffectiveSuggestions()

	assert.Equal(t, "openai/gpt-4o-mini", got.Model)
	assert.Equal(t, 6, got.ContextMessages)
	assert.Equal(t, 32, got.MaxTokens)
	assert.Equal(t, 5, got.Timeout)
}
