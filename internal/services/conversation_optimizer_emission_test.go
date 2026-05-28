package services_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	services "github.com/inference-gateway/cli/internal/services"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
)

// decodeStreamEvents parses each newline-terminated JSON line in buf into
// a map, in emission order. Empty / whitespace-only lines are skipped.
func decodeStreamEvents(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	events := []map[string]any{}
	for _, line := range strings.Split(buf.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var event map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		events = append(events, event)
	}
	return events
}

// fourMessages returns a small but valid conversation that survives the
// keepFirstMessages=2 boundary and forces smartOptimize to actually run.
func fourMessages() []sdk.Message {
	return []sdk.Message{
		{Role: "user", Content: sdk.NewMessageContent("first")},
		{Role: "assistant", Content: sdk.NewMessageContent("second")},
		{Role: "user", Content: sdk.NewMessageContent("third")},
		{Role: "assistant", Content: sdk.NewMessageContent("fourth")},
	}
}

// withDebugStreamWriter wires a buffer + forces the streamevent debug
// gate on for the lifetime of t.
func withDebugStreamWriter(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	t.Cleanup(streamevent.SetWriter(&buf))
	t.Cleanup(streamevent.SetDebugEnabledForTest(true))
	return &buf
}

func TestOptimizeMessages_EmitsStartedAndCompletedEvents_OnForce(t *testing.T) {
	mockClient := createMockSDKClient(t, "compact summary")
	optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
	})

	buf := withDebugStreamWriter(t)

	messages := fourMessages()
	result := optimizer.OptimizeMessages(messages, "deepseek/deepseek-v4-pro", true)
	require.NotEmpty(t, result, "optimizer must return a non-empty result")

	events := decodeStreamEvents(t, buf)
	require.Len(t, events, 2, "expected one started + one completed event")

	started := events[0]
	completed := events[1]

	assert.Equal(t, "compaction_started", started["type"], "operational events use type, not role")
	_, hasRole := started["role"]
	assert.False(t, hasRole, "operational events must not carry a role field")
	assert.EqualValues(t, true, started["force"], "force=true must be visible in started event")
	assert.EqualValues(t, 80, started["auto_at_pct"])
	assert.EqualValues(t, len(messages), started["messages_before"])
	assert.NotZero(t, started["threshold"], "threshold must be reported")
	assert.NotZero(t, started["context_window"], "context window must be reported")
	assert.NotEmpty(t, started["timestamp"])

	assert.Equal(t, "compaction_completed", completed["type"])
	assert.EqualValues(t, len(messages), completed["messages_before"])
	assert.NotZero(t, completed["messages_after"])
	assert.NotEmpty(t, completed["timestamp"])
}

func TestOptimizeMessages_NoEventsWhenBelowThreshold(t *testing.T) {
	mockClient := createMockSDKClient(t, "should not be called")
	optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
	})

	buf := withDebugStreamWriter(t)

	messages := fourMessages()
	result := optimizer.OptimizeMessages(messages, "deepseek/deepseek-v4-pro", false)
	assert.Equal(t, messages, result, "below-threshold call must return input unchanged")
	assert.Empty(t, buf.String(), "no events when threshold not crossed and force=false")
}

func TestOptimizeMessages_NoEventsWhenOptimizerDisabled(t *testing.T) {
	mockClient := createMockSDKClient(t, "ignored")
	optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:           false,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
	})

	buf := withDebugStreamWriter(t)

	messages := fourMessages()
	_ = optimizer.OptimizeMessages(messages, "deepseek/deepseek-v4-pro", false)
	assert.Empty(t, buf.String(), "disabled optimizer must not emit events on non-force path")
}

func TestOptimizeMessages_DebugGateOff_NoStreamEvents(t *testing.T) {
	mockClient := createMockSDKClient(t, "compact summary")
	optimizer := services.NewConversationOptimizer(services.OptimizerConfig{
		Enabled:           true,
		AutoAt:            80,
		BufferSize:        2,
		KeepFirstMessages: 2,
		Client:            mockClient,
		Config:            &config.Config{},
		Tokenizer:         nil,
	})

	var buf bytes.Buffer
	t.Cleanup(streamevent.SetWriter(&buf))
	t.Cleanup(streamevent.SetDebugEnabledForTest(false))

	_ = optimizer.OptimizeMessages(fourMessages(), "deepseek/deepseek-v4-pro", true)
	assert.Empty(t, buf.String(), "stream events must be suppressed when debug gate is off")
}
