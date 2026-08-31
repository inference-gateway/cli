package config

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	yaml "gopkg.in/yaml.v3"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func modeChangeQuery(changed bool, prev, cur agentdomain.AgentMode) agentdomain.ReminderQuery {
	return agentdomain.ReminderQuery{
		Hook:        agentdomain.HookPreStream,
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

	due := cfg.RemindersDue(modeChangeQuery(false, agentdomain.AgentModeStandard, agentdomain.AgentModeStandard))
	for _, r := range due {
		assert.NotEqual(t, DefaultModeChangeReminderName, r.Name, "must not fire without a mode change")
	}

	due = cfg.RemindersDue(modeChangeQuery(true, agentdomain.AgentModeAutoAccept, agentdomain.AgentModePlan))
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
		mode   agentdomain.AgentMode
		wantIn string
	}{
		{agentdomain.AgentModePlan, "read-only mode"},
		{agentdomain.AgentModeAutoAccept, "tool approvals are bypassed"},
		{agentdomain.AgentModeStandard, "per-call tool approvals"},
		{agentdomain.AgentModeReadOnly, "You are now in Read-Only mode."},
	}
	for _, tc := range cases {
		due := cfg.RemindersDue(modeChangeQuery(true, agentdomain.AgentModeStandard, tc.mode))
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
			Hook:     agentdomain.HookPreStream,
			Trigger:  ReminderTriggerOnModeChange,
			Text:     DefaultModeChangeReminderText,
			Guidance: map[string]string{"plan": "CUSTOM PLAN GUIDANCE"},
		}},
	}

	due := cfg.RemindersDue(modeChangeQuery(true, agentdomain.AgentModeStandard, agentdomain.AgentModePlan))
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "CUSTOM PLAN GUIDANCE")

	due = cfg.RemindersDue(modeChangeQuery(true, agentdomain.AgentModePlan, agentdomain.AgentModeAutoAccept))
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "tool approvals are bypassed", "unset keys keep built-in defaults")
}

// The whole entry is overridable by name through the standard merge path.
func TestModeChangeTrigger_MergeWithDefaultsOverridesEntry(t *testing.T) {
	user := RemindersConfig{
		Enabled: true,
		Reminders: []ReminderConfig{{
			Name:    DefaultModeChangeReminderName,
			Hook:    agentdomain.HookPreStream,
			Trigger: ReminderTriggerOnModeChange,
			Text:    "switched {prev_mode} -> {new_mode}",
		}},
	}
	merged := user.MergeWithDefaults()

	due := merged.RemindersDue(modeChangeQuery(true, agentdomain.AgentModeStandard, agentdomain.AgentModePlan))
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
		Hook:    agentdomain.HookPostTool,
		Trigger: ReminderTriggerOnModeChange,
	}}}
	assert.ErrorContains(t, bad.Validate(), "requires hook pre_stream")

	badKey := RemindersConfig{Reminders: []ReminderConfig{{
		Name:     "m",
		Text:     "t",
		Hook:     agentdomain.HookPreStream,
		Trigger:  ReminderTriggerOnModeChange,
		Guidance: map[string]string{"yolo": "x"},
	}}}
	assert.ErrorContains(t, badKey.Validate(), "unknown guidance mode key")

	assert.NoError(t, DefaultRemindersConfig().Validate())
}

// The plan-mode Markdown template is carried by the mode-change reminder
// guidance (not a per-mode system prompt). This is the contract with
// docs/plan-mode.md, moved from prompts_test.go.
func TestModeChangeTrigger_PlanGuidanceKeepsPlanFormat(t *testing.T) {
	plan := defaultModeChangeGuidance["plan"]
	require.NotEmpty(t, plan)

	wantSections := []string{
		"## Context",
		"## Files to Modify",
		"## Current Code",
		"## Changes",
		"## Performance Impact",
		"## Critical Files",
		"## Edge Cases",
		"## Verification",
	}
	for _, section := range wantSections {
		assert.Contains(t, plan, section, "plan guidance missing section heading %q", section)
	}

	for _, want := range []string{"RequestPlanApproval", "AskUserQuestion", "Read", "Grep", "Tree", "TodoWrite", "title"} {
		assert.Contains(t, plan, want)
	}

	for _, want := range []string{"DESTRUCTIVE-ACTION POLICY", "rm -rf"} {
		assert.Contains(t, defaultModeChangeGuidance["auto"], want)
	}
}

// Guidance precedence (resolveModeChangeText): prompts.yaml mode adjustments
// ("plan" key" on the query) override the built-in default, but a guidance key
// the user actually edited in reminders.yaml keeps precedence.
func TestModeChangeTrigger_ModeGuidancePrecedence(t *testing.T) {
	q := modeChangeQuery(true, agentdomain.AgentModeStandard, agentdomain.AgentModePlan)
	q.ModeGuidance = map[string]string{"plan": "PROMPTS-CONFIG PLAN ADJUSTMENT"}

	due := DefaultRemindersConfig().RemindersDue(q)
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "PROMPTS-CONFIG PLAN ADJUSTMENT",
		"prompts.yaml mode adjustments override the built-in guidance")

	user := DefaultRemindersConfig()
	for i := range user.Reminders {
		if user.Reminders[i].Name == DefaultModeChangeReminderName {
			user.Reminders[i].Guidance["plan"] = "USER-EDITED PLAN GUIDANCE"
		}
	}
	due = user.RemindersDue(q)
	require.Len(t, due, 1)
	assert.Contains(t, due[0].Text, "USER-EDITED PLAN GUIDANCE",
		"a user-edited reminders.yaml guidance key keeps precedence over prompts.yaml")
}
