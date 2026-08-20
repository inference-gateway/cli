package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	services "github.com/inference-gateway/cli/internal/services"
)

// newAnthropicEnv is newEnv pointed at the mock's Anthropic model, so the
// AnthropicMessages adapter routes the agent through POST /v1/messages.
func newAnthropicEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, func(cfg *config.Config) {
		cfg.Agent.Model = testAnthropicModel
		cfg.Prompts.Agent.SystemPrompt = "You are a test agent."
	})
	require.NoError(t, e.container.GetModelService().SelectModel(testAnthropicModel))
	return e
}

func (e *env) runAnthropicStream(ctx context.Context, t *testing.T, prompt string) result {
	t.Helper()
	req := &agentdomain.AgentRequest{
		RequestID: fmt.Sprintf("req-%s", strings.ReplaceAll(t.Name(), "/", "-")),
		Model:     testAnthropicModel,
		Messages:  []sdk.Message{userMessage(t, prompt)},
	}
	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)
	return drain(t, events)
}

// messagesBodies returns the recorded /v1/messages request bodies, failing if
// any recorded request went to another endpoint.
func (e *env) messagesBodies(t *testing.T) []sdk.CreateMessagesRequest {
	t.Helper()
	reqs := e.gateway.Requests()
	bodies := make([]sdk.CreateMessagesRequest, len(reqs))
	for i, r := range reqs {
		require.Equal(t, "/v1/messages", r.Endpoint, "anthropic model must never hit /v1/chat/completions")
		require.Equal(t, "anthropic", r.Provider)
		require.NoError(t, json.Unmarshal(r.RawBody, &bodies[i]))
	}
	return bodies
}

// cacheControlCount counts cache_control markers in the marshaled request:
// system block + last tool + the rolling conversation breakpoint.
func cacheControlCount(t *testing.T, req sdk.CreateMessagesRequest) int {
	t.Helper()
	b, err := json.Marshal(req)
	require.NoError(t, err)
	return strings.Count(string(b), `"cache_control"`)
}

// TestMessagesToolLoop is the end-to-end acceptance check for issue #942: the
// agent drives a full tool round-trip over the native Anthropic endpoint -
// thinking and text deltas reach the TUI event stream, tool arguments
// reassemble from input_json_delta fragments, tool results return as
// tool_result blocks, and every request carries the three cache_control
// breakpoints with the volatile <system-reminder> tail left uncached.
func TestMessagesToolLoop(t *testing.T) {
	e := newAnthropicEnv(t)
	e.writeFixtures(t, "a.txt")

	res := e.runAnthropicStream(context.Background(), t, "exercise the anthropic cache")

	require.Empty(t, res.errs)
	require.Equal(t, "Cache exercised.", res.content())
	require.Equal(t, "Deciding which file to inspect first.", res.reasoning())

	bodies := e.messagesBodies(t)
	require.Len(t, bodies, 2, "tool round-trip must trigger exactly one follow-up request")

	for i, body := range bodies {
		require.Equal(t, "claude-sonnet-4-5", body.Model, "provider prefix must be stripped (request %d)", i)
		require.Equal(t, 3, cacheControlCount(t, body), "system + last tool + rolling breakpoint (request %d)", i)

		require.NotNil(t, body.System, "system prompt must ride the top-level system field (request %d)", i)
		require.NotNil(t, body.Tools, "tools must be present (request %d)", i)
		tools := *body.Tools
		require.NotEmpty(t, tools)
		require.NotNil(t, tools[len(tools)-1].CacheControl, "last tool carries a breakpoint (request %d)", i)
		for _, tool := range tools[:len(tools)-1] {
			require.Nil(t, tool.CacheControl)
		}
	}

	followUp := bodies[1]
	toolResult := findToolResult(t, followUp, "call_0_0")
	require.Contains(t, toolResultText(t, toolResult), "fixture content")

	lastBlocks := requestBlocks(t, followUp.Messages[len(followUp.Messages)-1])
	tail, err := lastBlocks[len(lastBlocks)-1].AsMessagesTextBlock()
	require.NoError(t, err)
	require.Contains(t, tail.Text, "<system-reminder>")
	require.Nil(t, tail.CacheControl, "the volatile tail must never consume a cache breakpoint")
}

// TestMessagesTokenAccounting checks that Anthropic cache usage lands in the
// session stats with OpenAI semantics: prompt tokens are the synthesized
// input+read+write totals, cache reads accumulate as cached tokens and cache
// writes into the new TotalCacheWriteTokens counter.
func TestMessagesTokenAccounting(t *testing.T) {
	e := newAnthropicEnv(t)
	e.writeFixtures(t, "a.txt")

	res := e.runAnthropicStream(context.Background(), t, "exercise the anthropic cache")
	require.Empty(t, res.errs)

	stats := e.container.GetConversationRepository().GetSessionTokens()
	require.Equal(t, 490, stats.TotalInputTokens, "230 + 260 synthesized prompt tokens")
	require.Equal(t, 37, stats.TotalOutputTokens, "25 + 12 completion tokens")
	require.Equal(t, 120, stats.TotalCachedTokens, "turn-2 cache read must accumulate")
	require.Equal(t, 100, stats.TotalCacheWriteTokens, "turn-1 cache write must accumulate")
	require.Equal(t, 2, stats.RequestCount)
}

// TestMessagesCacheWritePricing verifies the gateway-advertised
// cache_write_per_token rate flows into cost calculation.
func TestMessagesCacheWritePricing(t *testing.T) {
	newAnthropicEnv(t)

	pricing := services.NewPricingService(&config.PricingConfig{Enabled: true})
	in, _, _ := pricing.CalculateCost(testAnthropicModel, 1_000_000, 0, 0, 1_000_000)
	require.InDelta(t, 3.125, in, 1e-9, "cache writes must bill at the cache-write rate")

	in, _, _ = pricing.CalculateCost(testAnthropicModel, 1_000_000, 0, 400_000, 600_000)
	require.InDelta(t, 0.4*0.25+0.6*3.125, in, 1e-9, "mixed read/write must bill each bucket at its rate")
}

// TestMessagesReasoningEffort verifies a runtime effort switch reaches the
// wire as output_config.effort on every subsequent /v1/messages request.
func TestMessagesReasoningEffort(t *testing.T) {
	e := newAnthropicEnv(t)
	e.writeFixtures(t, "a.txt")

	require.NoError(t, e.container.GetAgentService().SetReasoningEffort("xhigh"))

	res := e.runAnthropicStream(context.Background(), t, "exercise the anthropic cache")
	require.Empty(t, res.errs)

	bodies := e.messagesBodies(t)
	require.Len(t, bodies, 2)
	for i, body := range bodies {
		require.NotNil(t, body.OutputConfig, "output_config must be present (request %d)", i)
		require.NotNil(t, body.OutputConfig.Effort, "effort must be present (request %d)", i)
		require.Equal(t, sdk.MessagesOutputConfigEffortXhigh, *body.OutputConfig.Effort, "request %d", i)
	}
}

// TestMessagesSyncRun covers the non-streaming (headless) path over
// /v1/messages.
func TestMessagesSyncRun(t *testing.T) {
	e := newAnthropicEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	resp, err := e.container.GetAgentService().Run(ctx, &agentdomain.AgentRequest{
		RequestID: "req-messages-sync",
		Model:     testAnthropicModel,
		Messages:  []sdk.Message{userMessage(t, "say hello")},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help?", resp.Content)
	require.NotNil(t, resp.Usage)
	require.EqualValues(t, 15, resp.Usage.TotalTokens)

	reqs := e.gateway.Requests()
	require.Len(t, reqs, 1)
	require.False(t, reqs[0].Stream, "AgentService.Run must use the non-streaming path")
	require.Equal(t, "/v1/messages", reqs[0].Endpoint)
}

func requestBlocks(t *testing.T, m sdk.MessagesMessage) []sdk.MessagesRequestContentBlock {
	t.Helper()
	blocks, err := m.Content.AsMessagesMessageContent1()
	require.NoError(t, err, "expected block-array content")
	return blocks
}

func findToolResult(t *testing.T, req sdk.CreateMessagesRequest, toolUseID string) sdk.MessagesToolResultBlock {
	t.Helper()
	for _, m := range req.Messages {
		if m.Role != sdk.MessagesMessageRoleUser {
			continue
		}
		blocks, err := m.Content.AsMessagesMessageContent1()
		if err != nil {
			continue
		}
		for _, b := range blocks {
			if tr, err := b.AsMessagesToolResultBlock(); err == nil &&
				tr.Type == sdk.ToolResult && tr.ToolUseID == toolUseID {
				return tr
			}
		}
	}
	t.Fatalf("no tool_result block for %s", toolUseID)
	return sdk.MessagesToolResultBlock{}
}

func toolResultText(t *testing.T, tr sdk.MessagesToolResultBlock) string {
	t.Helper()
	require.NotNil(t, tr.Content)
	if s, err := tr.Content.AsMessagesToolResultBlockContent0(); err == nil {
		return s
	}
	blocks, err := tr.Content.AsMessagesToolResultBlockContent1()
	require.NoError(t, err)
	var b strings.Builder
	for _, tb := range blocks {
		b.WriteString(tb.Text)
	}
	return b.String()
}
