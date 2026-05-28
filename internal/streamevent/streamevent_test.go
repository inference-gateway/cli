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

func TestEmit_WritesSingleJSONLineWithRoleAndTimestamp(t *testing.T) {
	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	streamevent.Emit("system_reminder", map[string]any{
		"turn":     5,
		"interval": 5,
		"text":     "<system-reminder>nudge</system-reminder>",
	})

	out := buf.String()
	require.Equal(t, 1, strings.Count(out, "\n"), "should emit exactly one newline-terminated line")
	require.True(t, strings.HasSuffix(out, "\n"), "line must end with newline")

	var event map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &event))

	assert.Equal(t, "system_reminder", event["role"])
	assert.NotEmpty(t, event["timestamp"], "timestamp must be set")
	assert.EqualValues(t, 5, event["turn"])
	assert.EqualValues(t, 5, event["interval"])
	assert.Equal(t, "<system-reminder>nudge</system-reminder>", event["text"])
}

func TestEmit_CallerFieldsCanOverrideRoleAndTimestamp(t *testing.T) {
	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	streamevent.Emit("compaction_started", map[string]any{
		"role":      "should_win",
		"timestamp": "2026-05-28T00:00:00Z",
		"force":     true,
	})

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))

	assert.Equal(t, "should_win", event["role"], "caller override of role must win for test determinism")
	assert.Equal(t, "2026-05-28T00:00:00Z", event["timestamp"])
	assert.Equal(t, true, event["force"])
}

func TestEmit_NilFieldsStillProducesRoleAndTimestamp(t *testing.T) {
	var buf bytes.Buffer
	restore := streamevent.SetWriter(&buf)
	t.Cleanup(restore)

	streamevent.Emit("compaction_completed", nil)

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))

	assert.Equal(t, "compaction_completed", event["role"])
	assert.NotEmpty(t, event["timestamp"])
	assert.Len(t, event, 2, "no extra keys when fields is nil")
}

func TestSetWriter_RestoreReturnsPreviousWriter(t *testing.T) {
	var first, second bytes.Buffer

	restoreFirst := streamevent.SetWriter(&first)
	restoreSecond := streamevent.SetWriter(&second)
	streamevent.Emit("inner", map[string]any{"i": 1})
	restoreSecond()
	streamevent.Emit("outer", map[string]any{"o": 1})
	restoreFirst()

	assert.Contains(t, second.String(), `"role":"inner"`)
	assert.Contains(t, first.String(), `"role":"outer"`)
	assert.NotContains(t, first.String(), `"role":"inner"`)
	assert.NotContains(t, second.String(), `"role":"outer"`)
}
