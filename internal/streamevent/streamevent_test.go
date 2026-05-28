package streamevent_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	streamevent "github.com/inference-gateway/cli/internal/streamevent"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

// withDebugWriter wires a buffer and forces the debug gate on for the
// duration of a test. Both are restored on cleanup.
func withDebugWriter(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	restoreWriter := streamevent.SetWriter(&buf)
	restoreGate := streamevent.SetDebugEnabledForTest(true)
	t.Cleanup(restoreWriter)
	t.Cleanup(restoreGate)
	return &buf
}

func TestEmitDebugMessage_HiddenUserMessageShape(t *testing.T) {
	buf := withDebugWriter(t)

	streamevent.EmitDebugMessage("user", "<system-reminder>nudge</system-reminder>", "system_reminder", map[string]any{
		"turn":     5,
		"interval": 5,
	})

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, "\n"))

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))

	assert.Equal(t, "user", event["role"], "role must match the actual conversation role")
	assert.Equal(t, "<system-reminder>nudge</system-reminder>", event["content"])
	assert.Equal(t, true, event["hidden"], "reminder is a hidden conversation message")
	assert.Equal(t, "system_reminder", event["kind"])
	assert.EqualValues(t, 5, event["turn"])
	assert.EqualValues(t, 5, event["interval"])
	assert.NotEmpty(t, event["timestamp"])
}

func TestEmitDebugEvent_OperationalShape(t *testing.T) {
	buf := withDebugWriter(t)

	streamevent.EmitDebugEvent("compaction_started", map[string]any{
		"current_tokens": 24000,
		"threshold":      24000,
		"force":          false,
	})

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))

	assert.Equal(t, "compaction_started", event["type"], "operational events use type, not role")
	_, hasRole := event["role"]
	assert.False(t, hasRole, "operational events must not carry a role field")
	assert.EqualValues(t, 24000, event["current_tokens"])
	assert.EqualValues(t, 24000, event["threshold"])
	assert.Equal(t, false, event["force"])
	assert.NotEmpty(t, event["timestamp"])
}

func TestEmit_NoOpWhenDebugDisabled(t *testing.T) {
	var buf bytes.Buffer
	restoreWriter := streamevent.SetWriter(&buf)
	t.Cleanup(restoreWriter)
	// Explicitly force the gate off — production default.
	restoreGate := streamevent.SetDebugEnabledForTest(false)
	t.Cleanup(restoreGate)

	streamevent.EmitDebugMessage("user", "should not appear", "system_reminder", nil)
	streamevent.EmitDebugEvent("compaction_started", map[string]any{"x": 1})

	assert.Empty(t, buf.String(), "no events must be written when debug gate is off")
}

func TestSetWriter_RestoreReturnsPreviousWriter(t *testing.T) {
	restoreGate := streamevent.SetDebugEnabledForTest(true)
	t.Cleanup(restoreGate)

	var first, second bytes.Buffer

	restoreFirst := streamevent.SetWriter(&first)
	restoreSecond := streamevent.SetWriter(&second)
	streamevent.EmitDebugEvent("inner", map[string]any{"i": 1})
	restoreSecond()
	streamevent.EmitDebugEvent("outer", map[string]any{"o": 1})
	restoreFirst()

	assert.Contains(t, second.String(), `"type":"inner"`)
	assert.Contains(t, first.String(), `"type":"outer"`)
	assert.NotContains(t, first.String(), `"type":"inner"`)
	assert.NotContains(t, second.String(), `"type":"outer"`)
}
