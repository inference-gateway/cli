package handlers

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

type nextStepRunnerFake struct{ calls int }

func (r *nextStepRunnerFake) GenerateNextStepSuggestion(context.Context) string {
	r.calls++
	return "run the failing test"
}

type suggestionGateFake struct{ allow bool }

func (g *suggestionGateFake) CanRequestSuggestion() bool { return g.allow }

func newSuggestionTestHandler(t *testing.T, enabled bool) *ChatHandler {
	t.Helper()
	return setupTestChatHandler(t, func(
		_ *agentdomainmocks.FakeAgentService,
		_ *convmocks.FakeModelService,
		_ *agentdomainmocks.FakeToolService,
		_ *agentdomainmocks.FakeFileService,
		cfg *config.Config,
	) {
		cfg.Chat.Suggestions.Enabled = enabled
	}, statemanager.NewStore(false))
}

func TestChatHandler_shouldArmSuggestionTimer(t *testing.T) {
	quiet := agentdomain.ChatCompleteEvent{}

	tests := []struct {
		name     string
		enabled  bool
		runner   tui.NextStepSuggestionRunner
		complete agentdomain.ChatCompleteEvent
		enqueue  bool
		want     bool
	}{
		{"runner not wired", true, nil, quiet, false, false},
		{"suggestions disabled", false, &nextStepRunnerFake{}, quiet, false, false},
		{"quiet turn arms", true, &nextStepRunnerFake{}, quiet, false, true},
		{"cancelled turn", true, &nextStepRunnerFake{}, agentdomain.ChatCompleteEvent{Cancelled: true}, false, false},
		{"tool calls feed back in", true, &nextStepRunnerFake{}, agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{{}}}, false, false},
		{"messages queued", true, &nextStepRunnerFake{}, quiet, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newSuggestionTestHandler(t, tt.enabled)
			h.suggestions = tt.runner
			h.suggestionGate = &suggestionGateFake{allow: true}
			if tt.enqueue {
				h.messageQueue.Enqueue(sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("queued")}, convdomain.QueueSourceComposer, "request")
			}

			assert.Equal(t, tt.want, h.shouldArmSuggestionTimer(tt.complete))
		})
	}
}

func TestChatHandler_QuietWindowRequestsOnce(t *testing.T) {
	runner := &nextStepRunnerFake{}
	gate := &suggestionGateFake{allow: true}
	h := newSuggestionTestHandler(t, true)
	h.suggestions = runner
	h.suggestionGate = gate

	firstArm := h.suggestionGeneration + 1
	armMsg := h.HandleChatCompleteEvent(agentdomain.ChatCompleteEvent{})
	require.NotNil(t, armMsg)
	require.NotNil(t, h.Handle(tui.SuggestionArmEvent{}))
	require.NotNil(t, h.Handle(tui.SuggestionArmEvent{}))
	secondArm := h.suggestionGeneration

	assert.Nil(t, h.HandleSuggestionQuietEvent(tui.SuggestionQuietEvent{Generation: firstArm}), "stale tick from the duplicate ChatCompleteEvent is ignored")

	cmd := h.HandleSuggestionQuietEvent(tui.SuggestionQuietEvent{Generation: secondArm})
	require.NotNil(t, cmd)
	assert.Equal(t, tui.NextStepSuggestionEvent{Suggestion: "run the failing test"}, cmd())
	assert.Equal(t, 1, runner.calls)
}

func TestChatHandler_TypingAndGateBlockSuggestion(t *testing.T) {
	runner := &nextStepRunnerFake{}
	gate := &suggestionGateFake{allow: true}
	h := newSuggestionTestHandler(t, true)
	h.suggestions = runner
	h.suggestionGate = gate

	_ = h.armSuggestionTimer()
	armed := h.suggestionGeneration
	_ = h.Handle(tui.AutocompleteUpdateEvent{Text: "r", CursorPos: 1})
	assert.Nil(t, h.HandleSuggestionQuietEvent(tui.SuggestionQuietEvent{Generation: armed}), "typing invalidates the pending tick")

	_ = h.armSuggestionTimer()
	gate.allow = false
	assert.Nil(t, h.HandleSuggestionQuietEvent(tui.SuggestionQuietEvent{Generation: h.suggestionGeneration}), "gate refuses a busy or non-empty UI")

	assert.Equal(t, 0, runner.calls)
}
