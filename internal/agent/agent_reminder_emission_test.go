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

func TestInjectSystemReminderIfDue_EmitsEventAndAppendsMessage(t *testing.T) {
	cfg := remindersOnlyConfig(true, 2, "remember to push")
	svc := &AgentServiceImpl{config: cfg}

	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(4, &conv)

	assert.True(t, injected, "interval 2 with turn 4 must fire")
	require.Len(t, conv, 1, "reminder must be appended to conversation")
	assert.Equal(t, sdk.User, conv[0].Role)

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))
	assert.Equal(t, "system_reminder", event["role"])
	assert.EqualValues(t, 4, event["turn"])
	assert.EqualValues(t, 2, event["interval"])
	assert.Equal(t, "remember to push", event["text"])
	assert.NotEmpty(t, event["timestamp"])
}

func TestInjectSystemReminderIfDue_NoEventWhenDisabled(t *testing.T) {
	cfg := remindersOnlyConfig(false, 2, "ignored")
	svc := &AgentServiceImpl{config: cfg}

	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(4, &conv)

	assert.False(t, injected)
	assert.Empty(t, conv, "conversation must not be appended to")
	assert.Empty(t, buf.String(), "no stream event must be emitted")
}

func TestInjectSystemReminderIfDue_NoEventBetweenIntervals(t *testing.T) {
	cfg := remindersOnlyConfig(true, 5, "wait for turn 5")
	svc := &AgentServiceImpl{config: cfg}

	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	conv := []sdk.Message{}
	injected := svc.injectSystemReminderIfDue(3, &conv)

	assert.False(t, injected, "turn 3 mod interval 5 != 0, must not fire")
	assert.Empty(t, conv)
	assert.Empty(t, buf.String())
}
