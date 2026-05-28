package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
)

// remindersOnlyConfig is a minimal config that exercises only the
// SystemReminders gate. Other Prompts subtrees stay zero-valued.
func remindersOnlyConfig(enabled bool, interval int, text string) *config.Config {
	return &config.Config{
		Prompts: config.PromptsConfig{
			Agent: config.PromptsAgentConfig{
				SystemReminders: config.PromptsAgentRemindersConfig{
					Enabled:      enabled,
					Interval:     interval,
					ReminderText: text,
				},
			},
		},
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

func TestInjectSystemReminderIfDue_EmitsHiddenUserMessage(t *testing.T) {
	cfg := remindersOnlyConfig(true, 2, "remember to push")
	svc := &AgentServiceImpl{config: cfg}

	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(4, &conv)

	assert.True(t, injected, "interval 2 with turn 4 must fire")
	require.Len(t, conv, 1, "reminder must be appended to conversation")
	assert.Equal(t, sdk.User, conv[0].Role, "actual conversation role is user")

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))
	assert.Equal(t, "user", event["role"], "stream event role mirrors the conversation role")
	assert.Equal(t, "remember to push", event["content"])
	assert.Equal(t, true, event["hidden"], "reminder is a hidden message")
	assert.Equal(t, "system_reminder", event["kind"])
	assert.EqualValues(t, 4, event["turn"])
	assert.EqualValues(t, 2, event["interval"])
	assert.NotEmpty(t, event["timestamp"])
}

func TestInjectSystemReminderIfDue_NoEventWhenDisabled(t *testing.T) {
	cfg := remindersOnlyConfig(false, 2, "ignored")
	svc := &AgentServiceImpl{config: cfg}

	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(4, &conv)

	assert.False(t, injected)
	assert.Empty(t, conv, "conversation must not be appended to")
	assert.Empty(t, buf.String(), "no stream event must be emitted")
}

func TestInjectSystemReminderIfDue_NoEventBetweenIntervals(t *testing.T) {
	cfg := remindersOnlyConfig(true, 5, "wait for turn 5")
	svc := &AgentServiceImpl{config: cfg}

	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(3, &conv)

	assert.False(t, injected, "turn 3 mod interval 5 != 0, must not fire")
	assert.Empty(t, conv)
	assert.Empty(t, buf.String())
}

func TestInjectSystemReminderIfDue_DebugGateOff_NoStdoutButStillAppends(t *testing.T) {
	cfg := remindersOnlyConfig(true, 2, "remember to push")
	svc := &AgentServiceImpl{config: cfg}

	var buf bytes.Buffer
	t.Cleanup(streamevent.SetWriter(&buf))
	// Default = debug off
	t.Cleanup(streamevent.SetDebugEnabledForTest(false))

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(4, &conv)

	assert.True(t, injected, "reminder still injected into conversation regardless of stream gate")
	require.Len(t, conv, 1, "conversation still gets the message")
	assert.Empty(t, buf.String(), "but no stream event when debug gate is off")
}
