package handlers

import (
	"context"
	"testing"

	assert "github.com/stretchr/testify/assert"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
)

type nextStepRunnerFake struct{}

func (*nextStepRunnerFake) GenerateNextStepSuggestion(context.Context) string { return "" }

type suggestionGateFake struct{ allow bool }

func (g *suggestionGateFake) CanRequestSuggestion() bool { return g.allow }

func TestChatHandler_shouldRequestNextStepSuggestion(t *testing.T) {
	quiet := agentdomain.ChatCompleteEvent{}

	tests := []struct {
		name     string
		enabled  bool
		runner   tui.NextStepSuggestionRunner
		gate     tui.SuggestionGate
		complete agentdomain.ChatCompleteEvent
		enqueue  bool
		want     bool
	}{
		{"runner not wired", true, nil, &suggestionGateFake{allow: true}, quiet, false, false},
		{"suggestions disabled", false, &nextStepRunnerFake{}, &suggestionGateFake{allow: true}, quiet, false, false},
		{"quiet turn requests one", true, &nextStepRunnerFake{}, &suggestionGateFake{allow: true}, quiet, false, true},
		{"cancelled turn", true, &nextStepRunnerFake{}, &suggestionGateFake{allow: true}, agentdomain.ChatCompleteEvent{Cancelled: true}, false, false},
		{"tool calls feed back in", true, &nextStepRunnerFake{}, &suggestionGateFake{allow: true}, agentdomain.ChatCompleteEvent{ToolCalls: []sdk.ChatCompletionMessageToolCall{{}}}, false, false},
		{"messages queued", true, &nextStepRunnerFake{}, &suggestionGateFake{allow: true}, quiet, true, false},
		{"gate refuses non-empty input", true, &nextStepRunnerFake{}, &suggestionGateFake{allow: false}, quiet, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := setupTestChatHandler(t, func(
				_ *agentdomainmocks.FakeAgentService,
				_ *convmocks.FakeModelService,
				_ *agentdomainmocks.FakeToolService,
				_ *agentdomainmocks.FakeFileService,
				cfg *config.Config,
			) {
				cfg.Chat.Suggestions.Enabled = tt.enabled
			}, statemanager.NewStore(false))

			h.suggestions = tt.runner
			h.suggestionGate = tt.gate
			if tt.enqueue {
				h.messageQueue.Enqueue(sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent("queued")}, convdomain.QueueSourceComposer, "request")
			}

			assert.Equal(t, tt.want, h.shouldRequestNextStepSuggestion(tt.complete))
		})
	}
}
