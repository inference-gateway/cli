package config

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v3"

	domain "github.com/inference-gateway/cli/internal/domain"
)

func modeChangeQuery(changed bool, prev, cur domain.AgentMode) domain.ReminderQuery {
	return domain.ReminderQuery{
		Hook:        domain.HookPreStream,
		Turn:        2,
		SessionTurn: 1,
		ModeChanged: changed,
		PrevMode:    prev,
		Mode:        cur,
	}
}

// The on_mode_change trigger fires only when the query reports a change; in
// particular the default entry must NOT fire on an ordinary pre_stream turn
// (regression for the trigger:always raw-template leak).
func TestModeChangeTrigger_FiresOnlyOnChange(t *testing.T) {
	cfg := DefaultRemindersConfig()

	due := cfg.RemindersDue(modeChangeQuery(false, domain.AgentModeStandard, domain.AgentModeStandard))
	for _, r := range due {
		assert.NotEqual(t, DefaultModeChangeReminderName, r.Name, "must not fire without a mode change")
	}

	due = cfg.RemindersDue(modeChangeQuery(true, domain.AgentModeAutoAccept, domain.AgentModePlan))
	var texts []string
	for _, r := range due {
		if r.Name == DefaultModeChangeReminderName {
			texts = append(texts, r.Text)
		}
	}
	require.Len(t, texts, 1)
	assert.Contains(t, texts[0], "Auto-Accept")
	assert.Contains(t, texts[0], "Plan Mode")
	assert.Contains(t, texts[0], "do NOT attempt to make changes")
	assert.NotContains(t, texts[0], "{prev_mode}")
	assert.NotContains(t, texts[0], "{new_mode}")
	assert.NotContains(t, texts[0], "{guidance}")
}

// Per-mode guidance substitution: each target mode picks its own guidance text,
// and an unknown mode falls back to a generic sentence.
func TestModeChangeTrigger_GuidancePerMode(t *testing.T) {
	cfg := DefaultRemindersConfig()
	cases := []struct {
		mode   domain.AgentMode
		wantIn string
	}{
		{domain.AgentModePlan, "read-only mode"},
		{domain.AgentModeAutoAccept, "tool approvals are bypassed"},
		{domain.AgentModeStandard, "per-call tool approvals"},
		{domain.AgentModeReadOnly, "You are now in Read-Only mode."},
	}
	for _, tc := range cases {
		due := cfg.RemindersDue(modeChangeQuery(true, domain.AgentModeStandard, tc.mode))
		var found bool
		for _, r := range due {
			if r.Name == DefaultModeChangeReminderName {
				found = true
				assert.Contains(t, r.Text, tc.wantIn, tc.mode.String())
			}
		}
		assert.True(t, found, tc.mode.String())
	}
}

// A user override of a single guidance key keeps the built-in defaults for the
// other keys (per-key merge in effective()).
func TestModeChangeTrigger_GuidanceUserOverrideMergesPerKey(t *testing.T) {
	cfg := RemindersConfig{
		Enabled: true,
		Reminders: []ReminderConfig{{
			Name:     DefaultModeChangeReminderName,
			Hook:     domain.HookPreStream,
			Trigger:  ReminderTriggerOnModeChange,
			Text:     DefaultModeChangeReminderText,
			Guidance: map[string]string{"plan": "CUSTOM PLAN GUIDANCE"},
		}},
	}

	due := cfg.RemindersDue(modeChangeQuery(true, domain.AgentModeStandard, domain.AgentModePlan))
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "CUSTOM PLAN GUIDANCE")

	due = cfg.RemindersDue(modeChangeQuery(true, domain.AgentModePlan, domain.AgentModeAutoAccept))
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "tool approvals are bypassed", "unset keys keep built-in defaults")
}

// The whole entry is overridable by name through the standard merge path.
func TestModeChangeTrigger_MergeWithDefaultsOverridesEntry(t *testing.T) {
	user := RemindersConfig{
		Enabled: true,
		Reminders: []ReminderConfig{{
			Name:    DefaultModeChangeReminderName,
			Hook:    domain.HookPreStream,
			Trigger: ReminderTriggerOnModeChange,
			Text:    "switched {prev_mode} -> {new_mode}",
		}},
	}
	merged := user.MergeWithDefaults()

	due := merged.RemindersDue(modeChangeQuery(true, domain.AgentModeStandard, domain.AgentModePlan))
	require.Len(t, due, 1)
	assert.Equal(t, "switched Standard -> Plan Mode", due[0].Text)
}

// The default entry must carry the per-mode guidance map so init seeds it into
// reminders.yaml where users can discover and edit the texts.
func TestModeChangeTrigger_DefaultGuidanceSeededAndRoundTrips(t *testing.T) {
	cfg := DefaultRemindersConfig()

	out, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(out), "guidance:")

	var loaded RemindersConfig
	require.NoError(t, yaml.Unmarshal(out, &loaded))
	for _, rc := range loaded.Reminders {
		if rc.Name == DefaultModeChangeReminderName {
			assert.Equal(t, defaultModeChangeGuidance, rc.Guidance)
			return
		}
	}
	t.Fatal("mode-change-reminder entry missing after round-trip")
}

func TestModeChangeTrigger_Validate(t *testing.T) {
	bad := RemindersConfig{Reminders: []ReminderConfig{{
		Name:    "m",
		Text:    "t",
		Hook:    domain.HookPostTool,
		Trigger: ReminderTriggerOnModeChange,
	}}}
	assert.ErrorContains(t, bad.Validate(), "requires hook pre_stream")

	badKey := RemindersConfig{Reminders: []ReminderConfig{{
		Name:     "m",
		Text:     "t",
		Hook:     domain.HookPreStream,
		Trigger:  ReminderTriggerOnModeChange,
		Guidance: map[string]string{"yolo": "x"},
	}}}
	assert.ErrorContains(t, badKey.Validate(), "unknown guidance mode key")

	assert.NoError(t, DefaultRemindersConfig().Validate())
}
