package conversation

import (
	"context"
	"strings"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdkmocks "github.com/inference-gateway/cli/tests/mocks/sdk"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
)

func TestSanitizeNextStepSuggestion(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected string
	}{
		{"plain line", "run the failing test", "run the failing test"},
		{"surrounding double quotes", `"run the failing test"`, "run the failing test"},
		{"surrounding single quotes", "'run the failing test'", "run the failing test"},
		{"surrounding backticks", "`run the failing test`", "run the failing test"},
		{"fenced one-liner", "```text\nrun the failing test\n```", "run the failing test"},
		{"empty", "   ", ""},
		{"multi-line discarded", "run the failing test\nthen commit", ""},
		{"multi-line fenced discarded", "```\nrun the failing test\nthen commit\n```", ""},
		{"over-limit truncated", strings.Repeat("word ", 24), strings.Repeat("word ", 15) + "word"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suggestion := SanitizeNextStepSuggestion(tt.raw)
			assert.Equal(t, tt.expected, suggestion)
			assert.LessOrEqual(t, len(suggestion), maxSuggestionChars)
		})
	}
}

func TestChatSuggestionGenerator_DisabledOrMissingModelSkipsCall(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)
	require.NoError(t, repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("why is the build failing")},
		Time:    time.Now(),
	}))

	client := &sdkmocks.FakeClient{}

	disabled := NewChatSuggestionGenerator(client, repo, &config.Config{Agent: config.AgentConfig{Model: "anthropic/claude-4-haiku"}}, nil, nil)
	assert.Equal(t, "", disabled.GenerateNextStepSuggestion(context.Background()))
	assert.Equal(t, 0, client.GenerateContentCallCount())

	enabled := NewChatSuggestionGenerator(client, repo, &config.Config{
		Chat: config.ChatConfig{Suggestions: config.ChatSuggestionsConfig{Enabled: true}},
	}, nil, nil)
	assert.Equal(t, "", enabled.GenerateNextStepSuggestion(context.Background()))
	assert.Equal(t, 0, client.GenerateContentCallCount())
}

func TestChatSuggestionGenerator_GenerateNextStepSuggestionReturnsSanitizedLine(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)
	now := time.Now()
	require.NoError(t, repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("the CLI tests are red")},
		Time:    now,
	}))
	require.NoError(t, repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("One assertion expects exit code 0 but the test fails it.")},
		Time:    now,
	}))

	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentStub = func(_ context.Context, provider sdk.Provider, model string, messages []sdk.Message) (*sdk.CreateChatCompletionResponse, error) {
		assert.Equal(t, "claude-4-haiku", model)
		assert.Len(t, messages, 2)
		assert.Equal(t, sdk.Provider("anthropic"), provider)
		assert.Equal(t, sdk.System, messages[0].Role)
		assert.Equal(t, sdk.User, messages[1].Role)
		return &sdk.CreateChatCompletionResponse{
			Choices: []sdk.ChatCompletionChoice{
				{Message: sdk.Message{Content: sdk.NewMessageContent("\"run go test ./internal/conversation\"")}},
			},
			Usage: &sdk.CompletionUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		}, nil
	}

	cfg := &config.Config{
		Chat:    config.ChatConfig{Suggestions: config.ChatSuggestionsConfig{Enabled: true}},
		Prompts: *config.DefaultPromptsConfig(),
	}
	activeModel := func() string { return "anthropic/claude-4-haiku" }
	generator := NewChatSuggestionGenerator(client, repo, cfg, activeModel, nil)

	suggestion := generator.GenerateNextStepSuggestion(context.Background())

	assert.Equal(t, "run go test ./internal/conversation", suggestion)

	tokens := repo.GetSessionTokens()
	assert.Equal(t, 120, tokens.TotalTokens)
	assert.Equal(t, 1, tokens.RequestCount)
}

func TestChatSuggestionGenerator_DiscardsAnswerCutByMaxTokens(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)
	require.NoError(t, repo.AddMessage(convdomain.ConversationEntry{
		Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("how do I install it")},
		Time:    time.Now(),
	}))

	client := &sdkmocks.FakeClient{}
	client.WithOptionsReturns(client)
	client.WithMiddlewareOptionsReturns(client)
	client.GenerateContentReturns(&sdk.CreateChatCompletionResponse{
		Choices: []sdk.ChatCompletionChoice{
			{FinishReason: sdk.Length, Message: sdk.Message{Content: sdk.NewMessageContent("Show how to install and")}},
		},
		Usage: &sdk.CompletionUsage{PromptTokens: 200, CompletionTokens: 64, TotalTokens: 264},
	}, nil)

	cfg := &config.Config{
		Agent:   config.AgentConfig{Model: "ollama_cloud/deepseek"},
		Chat:    config.ChatConfig{Suggestions: config.ChatSuggestionsConfig{Enabled: true}},
		Prompts: *config.DefaultPromptsConfig(),
	}
	generator := NewChatSuggestionGenerator(client, repo, cfg, nil, nil)

	assert.Equal(t, "", generator.GenerateNextStepSuggestion(context.Background()))
	assert.Equal(t, 264, repo.GetSessionTokens().TotalTokens, "the cut call still counts toward session usage")
}

func TestChatSuggestionGenerator_suggestionDialogReducesToolPayloads(t *testing.T) {
	repo := NewInMemoryConversationRepository(nil, nil)
	now := time.Now()
	toolCalls := []sdk.ChatCompletionMessageToolCall{{Function: sdk.ChatCompletionMessageToolCallFunction{Name: "Read"}}}
	messages := []convdomain.ConversationEntry{
		{Message: sdk.Message{Role: sdk.System, Content: sdk.NewMessageContent("hidden system")}, Hidden: true, Time: now},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("older message beyond the window")}, Time: now},
		{Message: sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("latest user question")}, Time: now},
		{Message: sdk.Message{Role: sdk.Assistant, ToolCalls: &toolCalls}, Time: now},
		{Message: sdk.Message{Role: sdk.Tool, Content: sdk.NewMessageContent("file body that arrived as the tool result")}, Time: now},
		{Message: sdk.Message{Role: sdk.Assistant, Content: sdk.NewMessageContent("final answer")}, Time: now},
	}
	for _, entry := range messages {
		require.NoError(t, repo.AddMessage(entry))
	}

	generator := NewChatSuggestionGenerator(nil, repo, &config.Config{}, nil, nil)
	dialog := generator.suggestionDialog(4)

	assert.Contains(t, dialog, "User: latest user question")
	assert.Contains(t, dialog, "Assistant ran tools: Read")
	assert.Contains(t, dialog, "Tool result: file body that arrived as the tool result")
	assert.Contains(t, dialog, "Assistant: final answer")
	assert.NotContains(t, dialog, "older message beyond the window")
	assert.NotContains(t, dialog, "hidden system")
}
