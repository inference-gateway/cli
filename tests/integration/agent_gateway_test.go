// Package integration exercises the agent end-to-end against a mock
// inference-gateway over real HTTP: real SDK client, real SSE parsing and
// tool-call accumulation, real state machine and tool execution - no
// interface fakes on the LLM path (issue #815).
package integration

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	mockgateway "github.com/inference-gateway/cli/internal/mockgateway"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
)

const runTimeout = 30 * time.Second

type env struct {
	container *container.ServiceContainer
	gateway   *mockgateway.Server
}

// newEnv builds a real service container pointed at an in-process mock
// gateway, with the working directory moved to a fresh temp dir so tool
// executions (Read, Grep, ...) stay sandboxed to fixtures.
func newEnv(t *testing.T) *env {
	t.Helper()
	t.Chdir(t.TempDir())
	restore := streamevent.SetWriter(io.Discard)
	t.Cleanup(func() { restore() })

	gw := mockgateway.New(mockgateway.Default())
	ts := httptest.NewServer(gw)
	t.Cleanup(ts.Close)

	cfg := config.DefaultConfig()
	cfg.Gateway.URL = ts.URL
	cfg.Gateway.Run = false
	cfg.Storage.Enabled = false
	cfg.Agent.Model = mockgateway.DefaultModel
	cfg.Client.Retry.InitialBackoffSec = 0
	cfg.Client.Retry.MaxAttempts = 3
	cfg.Client.StallThresholdSec = 1

	c := container.NewServiceContainer(cfg)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = c.Shutdown(ctx)
	})

	models, err := c.GetModelService().ListModels(context.Background())
	require.NoError(t, err, "mock /v1/models must serve the real ModelService")
	require.Equal(t, []string{mockgateway.DefaultModel}, models)
	require.NoError(t, c.GetModelService().SelectModel(mockgateway.DefaultModel))

	return &env{container: c, gateway: gw}
}

func (e *env) writeFixtures(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		require.NoError(t, os.WriteFile(filepath.Join(".", name), []byte("fixture content with a TODO marker\n"), 0o600))
	}
}

func userMessage(t *testing.T, text string) sdk.Message {
	t.Helper()
	msg, err := sdk.NewTextMessage(sdk.User, text)
	require.NoError(t, err)
	return msg
}

type result struct {
	chunks    []domain.ChatChunkEvent
	completes []domain.ChatCompleteEvent
	errs      []domain.ChatErrorEvent
}

// content concatenates all streamed content deltas.
func (r result) content() string {
	var b strings.Builder
	for _, c := range r.chunks {
		b.WriteString(c.Content)
	}
	return b.String()
}

// reasoning concatenates all streamed reasoning deltas.
func (r result) reasoning() string {
	var b strings.Builder
	for _, c := range r.chunks {
		b.WriteString(c.ReasoningContent)
	}
	return b.String()
}

// final returns the last ChatCompleteEvent of the run.
func (r result) final(t *testing.T) domain.ChatCompleteEvent {
	t.Helper()
	require.NotEmpty(t, r.completes, "no ChatCompleteEvent received")
	return r.completes[len(r.completes)-1]
}

// runStream drives one full RunWithStream session (the state machine loops
// through tool execution internally) and drains events until the agent
// closes the channel.
func (e *env) runStream(ctx context.Context, t *testing.T, prompt string) result {
	t.Helper()
	req := &domain.AgentRequest{
		RequestID: fmt.Sprintf("req-%s", strings.ReplaceAll(t.Name(), "/", "-")),
		Model:     mockgateway.DefaultModel,
		Messages:  []sdk.Message{userMessage(t, prompt)},
	}

	events, err := e.container.GetAgentService().RunWithStream(ctx, req)
	require.NoError(t, err)
	return drain(t, events)
}

func drain(t *testing.T, events <-chan domain.ChatEvent) result {
	t.Helper()
	var res result
	deadline := time.After(runTimeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				return res
			}
			switch e := ev.(type) {
			case domain.ChatChunkEvent:
				res.chunks = append(res.chunks, e)
			case domain.ChatCompleteEvent:
				res.completes = append(res.completes, e)
			case domain.ChatErrorEvent:
				res.errs = append(res.errs, e)
			}
		case <-deadline:
			t.Fatal("timed out waiting for the agent event channel to close")
		}
	}
}

// completionBodies returns the recorded chat-completion request bodies.
func (e *env) completionBodies() []sdk.CreateChatCompletionRequest {
	reqs := e.gateway.Requests()
	bodies := make([]sdk.CreateChatCompletionRequest, len(reqs))
	for i, r := range reqs {
		bodies[i] = r.Body
	}
	return bodies
}

func toolMessages(body sdk.CreateChatCompletionRequest) []sdk.Message {
	var out []sdk.Message
	for _, m := range body.Messages {
		if m.Role == sdk.Tool {
			out = append(out, m)
		}
	}
	return out
}

func lastAssistant(t *testing.T, body sdk.CreateChatCompletionRequest) sdk.Message {
	t.Helper()
	for i := len(body.Messages) - 1; i >= 0; i-- {
		if body.Messages[i].Role == sdk.Assistant {
			return body.Messages[i]
		}
	}
	t.Fatal("no assistant message in request body")
	return sdk.Message{}
}

func TestStreamTextOnlyCompletesInOneRequest(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "say hello")

	require.Empty(t, res.errs)
	require.Equal(t, "Hello! How can I help?", res.content())
	require.Empty(t, res.final(t).ToolCalls)
	require.Len(t, e.gateway.Requests(), 1, "a text-only streaming turn must not loop")
}

func TestStreamToolCallArgumentsAreReassembled(t *testing.T) {
	e := newEnv(t)
	e.writeFixtures(t, "haystack.txt")

	res := e.runStream(context.Background(), t, "please search for TODO")

	require.Empty(t, res.errs)
	require.Equal(t, "Search finished: found matches for TODO.", res.content())

	bodies := e.completionBodies()
	require.Len(t, bodies, 2, "tool round-trip must trigger exactly one follow-up request")

	assistant := lastAssistant(t, bodies[1])
	require.NotNil(t, assistant.ToolCalls)
	calls := *assistant.ToolCalls
	require.Len(t, calls, 1)
	require.Equal(t, "Grep", calls[0].Function.Name)
	require.JSONEq(t, `{"pattern":"TODO","path":"."}`, calls[0].Function.Arguments,
		"fragmented SSE arguments must reassemble into the full JSON")

	tools := toolMessages(bodies[1])
	require.Len(t, tools, 1)
	require.Equal(t, calls[0].ID, *tools[0].ToolCallID)
}

func TestStreamParallelReadsExecuteAllFourInOrder(t *testing.T) {
	e := newEnv(t)
	e.writeFixtures(t, "a.txt", "b.txt", "c.txt", "d.txt")

	res := e.runStream(context.Background(), t, "please execute the Read tool 4 times in parallel")

	require.Empty(t, res.errs)
	require.Equal(t, "All four files read.", res.content())

	bodies := e.completionBodies()
	require.Len(t, bodies, 2)

	assistant := lastAssistant(t, bodies[1])
	require.NotNil(t, assistant.ToolCalls)
	require.Len(t, *assistant.ToolCalls, 4)

	tools := toolMessages(bodies[1])
	require.Len(t, tools, 4, "all four tool results must return to the gateway")
	for i, call := range *assistant.ToolCalls {
		require.Equal(t, call.ID, *tools[i].ToolCallID, "tool results must keep call order")
	}
}

func TestStreamSequentialToolTurns(t *testing.T) {
	e := newEnv(t)
	e.writeFixtures(t, "a.txt")

	res := e.runStream(context.Background(), t, "explore the project structure")

	require.Empty(t, res.errs)
	require.Equal(t, "The project contains a.txt with fixture content.", res.content())
	require.Len(t, e.gateway.Requests(), 3, "Tree turn, Read turn, then the final answer")
}

func TestStreamReasoningAccumulatesSeparately(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "think step by step")

	require.Empty(t, res.errs)
	require.Equal(t, "Answer: 42.", res.content())
	require.Equal(t, "Considering the question carefully before answering.", res.reasoning())
}

func TestStreamUsageIsCaptured(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "report your usage")

	require.Empty(t, res.errs)
	final := res.final(t)
	require.NotNil(t, final.Metrics)
	require.NotNil(t, final.Metrics.Usage)
	require.EqualValues(t, 142, final.Metrics.Usage.TotalTokens)
}

func TestStreamRetriesOn429ThenRecovers(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "you are a flaky backend")

	require.Empty(t, res.errs)
	require.Equal(t, "Recovered after retries.", res.content())
	require.Len(t, e.gateway.Requests(), 3, "two injected 429s then success")
}

func TestStreamSurfacesHardErrorAfterRetryExhaustion(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "this always fails")

	require.NotEmpty(t, res.errs, "persistent 500s must surface as a ChatErrorEvent")
	require.Len(t, e.gateway.Requests(), 3, "initial request plus two retries")
}

func TestStreamStallReconnectsAndRecovers(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "please stall the stream")

	require.Empty(t, res.errs)
	require.Equal(t, "Reconnected after the stall.", res.content())
	require.Len(t, e.gateway.Requests(), 2, "stalled first attempt then one reconnect")
}

func TestStreamConnectHangReconnectsAndRecovers(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "please hang the connection")

	require.Empty(t, res.errs)
	require.Equal(t, "Connected after the hang.", res.content())
	require.Len(t, e.gateway.Requests(), 2, "hung connect then one reconnect")
}

func TestStreamMalformedFrameIsSkipped(t *testing.T) {
	e := newEnv(t)

	res := e.runStream(context.Background(), t, "give me a garbled stream")

	require.Empty(t, res.errs)
	require.Equal(t, "Still standing.", res.content(),
		"a malformed SSE frame is skipped and the stream continues")
}

func TestStreamCancelMidStreamReturnsPromptly(t *testing.T) {
	e := newEnv(t)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	res := e.runStream(ctx, t, "please stream slowly")

	require.Less(t, time.Since(start), 10*time.Second, "cancel must not wait for the full slow stream")
	if len(res.completes) > 0 {
		require.True(t, res.final(t).Cancelled, "a cancelled run must be marked Cancelled")
	}
}

func TestSyncRunParsesNonStreamingResponse(t *testing.T) {
	e := newEnv(t)

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	resp, err := e.container.GetAgentService().Run(ctx, &domain.AgentRequest{
		RequestID: "req-sync",
		Model:     mockgateway.DefaultModel,
		Messages:  []sdk.Message{userMessage(t, "say hello")},
	})
	require.NoError(t, err)
	require.Equal(t, "Hello! How can I help?", resp.Content)
	require.NotNil(t, resp.Usage)
	require.EqualValues(t, 15, resp.Usage.TotalTokens)

	reqs := e.gateway.Requests()
	require.Len(t, reqs, 1)
	require.False(t, reqs[0].Stream, "AgentService.Run must use the non-streaming path")
	require.Equal(t, "gpt-4o", reqs[0].Model, "provider prefix must be stripped from the wire model")
	require.Equal(t, "openai", reqs[0].Provider)
}

// TestSyncAndStreamAccumulateIdenticalSessionTokens is the acceptance check for
// issue #835: both the sync (headless) and streaming (chat) paths funnel through
// the same storeIterationMetrics accumulator, so for an identical scenario the
// session totals in the shared conversation repository must match.
func TestSyncAndStreamAccumulateIdenticalSessionTokens(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	syncEnv := newEnv(t)
	resp, err := syncEnv.container.GetAgentService().Run(ctx, &domain.AgentRequest{
		RequestID: "req-usage-sync",
		Model:     mockgateway.DefaultModel,
		Messages:  []sdk.Message{userMessage(t, "report your usage")},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Usage)
	require.EqualValues(t, 142, resp.Usage.TotalTokens)

	syncStats := syncEnv.container.GetConversationRepository().GetSessionTokens()
	require.Equal(t, 142, syncStats.TotalTokens, "sync Run must accumulate into the shared session sink")
	require.Equal(t, 100, syncStats.TotalInputTokens)
	require.Equal(t, 42, syncStats.TotalOutputTokens)
	require.Equal(t, 1, syncStats.RequestCount)

	streamEnv := newEnv(t)
	res := streamEnv.runStream(ctx, t, "report your usage")
	require.Empty(t, res.errs)

	streamStats := streamEnv.container.GetConversationRepository().GetSessionTokens()
	require.Equal(t, streamStats, syncStats, "headless totals must equal chat totals for the same scenario")
}
