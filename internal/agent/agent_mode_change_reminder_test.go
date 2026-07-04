package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// modeChangeSvc builds an agent service backed by the real default reminders
// config (which carries the on_mode_change entry) and a fake state manager
// reporting the given live mode.
func modeChangeSvc(enabled bool, liveMode domain.AgentMode) *AgentServiceImpl {
	sm := &domainmocks.FakeStateManager{}
	sm.GetAgentModeReturns(liveMode)
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	cfg.Reminders.Enabled = enabled
	return &AgentServiceImpl{stateManager: sm, config: cfg}
}

// The first pre_stream dispatch seeds the mode baseline without injecting -
// there is no "previous mode" to report a change from.
func TestModeChangeReminder_FirstTurnSeedsNoInject(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModePlan)
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreStream)

	assert.Empty(t, conv, "first turn must seed, not inject")
	assert.Empty(t, buf.String(), "no stream event on the seeding turn")
	assert.True(t, svc.modeInitialized, "mode must be marked initialized after the seed")
	assert.Equal(t, domain.AgentModePlan, svc.lastStreamedMode)
}

// Steady state: consecutive same-mode turns inject nothing, and no injected
// message may ever carry a raw template placeholder. This is the regression
// test for the trigger:always bug where the unformatted template leaked into
// the conversation on every turn.
func TestModeChangeReminder_SameModeNoInjectAndNoRawTemplate(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModeStandard)
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	ctx := newReminderAgentCtx(&conv, 1, 0)
	svc.injectDueReminders(ctx, domain.HookPreStream)
	svc.injectDueReminders(ctx, domain.HookPreStream)
	svc.injectDueReminders(ctx, domain.HookPreStream)

	assert.Empty(t, conv, "no change means no injection")
	for _, m := range conv {
		content, _ := m.Content.AsMessageContent0()
		assert.NotContains(t, content, "{prev_mode}", "raw template must never leak")
	}
}

// The core acceptance criterion: switching Auto -> Plan mid-session injects one
// hidden user message carrying the formatted new-mode guidance and emits a
// system_reminder stream event tagged with the reminder name.
func TestModeChangeReminder_AutoToPlanInjectsAndEmits(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = domain.AgentModeAutoAccept
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 5, 0), domain.HookPreStream)

	require.Len(t, conv, 1, "exactly one reminder must be appended")
	assert.Equal(t, sdk.User, conv[0].Role, "reminder is a hidden user message")

	content, _ := conv[0].Content.AsMessageContent0()
	assert.Contains(t, content, "Auto-Accept")
	assert.Contains(t, content, "Plan Mode")
	assert.Contains(t, content, "do NOT attempt to make changes")
	assert.NotContains(t, content, "{prev_mode}")
	assert.NotContains(t, content, "{guidance}")

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))
	assert.Equal(t, "user", event["role"])
	assert.Equal(t, "system_reminder", event["kind"])
	assert.Equal(t, config.DefaultModeChangeReminderName, event["name"])
	assert.EqualValues(t, 5, event["turn"])

	assert.Equal(t, domain.AgentModePlan, svc.lastStreamedMode, "lastStreamedMode advances to the new mode")
}

// The persisted ConversationEntry must be flagged Hidden so the mode-change
// reminder never surfaces as a visible user message in history.
func TestModeChangeReminder_PersistsHiddenViaRepo(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = domain.AgentModeStandard
	repo := &domainmocks.FakeConversationRepository{}
	repo.AddMessageReturns(nil)
	svc.conversationRepo = repo
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 2, 0), domain.HookPreStream)

	require.Equal(t, 1, repo.AddMessageCallCount(), "reminder must be persisted once")
	entry := repo.AddMessageArgsForCall(0)
	assert.True(t, entry.Hidden, "persisted entry must be hidden")
	assert.Equal(t, sdk.User, entry.Message.Role)
}

// A nil stateManager (the headless/agent Run path has no mode) never reports a
// change - no injection and no seeded tracking state.
func TestModeChangeReminder_NoOpWithoutStateManager(t *testing.T) {
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	svc := &AgentServiceImpl{config: cfg}
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreStream)

	assert.Empty(t, conv)
	assert.Empty(t, buf.String())
	assert.False(t, svc.modeInitialized, "must not seed state when the manager is absent")
}

// Reminders are user messages; while an assistant turn awaits tool results
// injection is skipped entirely, and mode tracking must not advance so the
// change still fires on the next clean turn.
func TestModeChangeReminder_SkipsWhenAwaitingToolResults(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = domain.AgentModeStandard
	withDebugStreamWriter(t)

	toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
	conv := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("go")},
		{Role: sdk.Assistant, ToolCalls: &toolCalls},
	}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 2, 0), domain.HookPreStream)

	assert.Len(t, conv, 2, "must not inject while awaiting tool results")
	assert.Equal(t, domain.AgentModeStandard, svc.lastStreamedMode, "tracking must not advance when skipped")
}

// After a change is recorded, the new mode becomes the baseline; a later turn
// in that same mode must not re-inject.
func TestModeChangeReminder_BaselineAdvancesAfterChange(t *testing.T) {
	sm := &domainmocks.FakeStateManager{}
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	svc := &AgentServiceImpl{stateManager: sm, config: cfg}
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	ctx := newReminderAgentCtx(&conv, 1, 0)

	sm.GetAgentModeReturns(domain.AgentModeAutoAccept)
	svc.injectDueReminders(ctx, domain.HookPreStream)
	require.Empty(t, conv, "turn 1 seeds")

	sm.GetAgentModeReturns(domain.AgentModePlan)
	svc.injectDueReminders(ctx, domain.HookPreStream)
	require.Len(t, conv, 1, "Auto -> Plan injects once")

	svc.injectDueReminders(ctx, domain.HookPreStream)
	require.Len(t, conv, 1, "must not re-inject once the new mode is the baseline")
}

// Disabling reminders disables the mode-change reminder too - it is an
// ordinary entry in the reminders config, gated by the master switch.
func TestModeChangeReminder_SkipsWhenRemindersDisabled(t *testing.T) {
	svc := modeChangeSvc(false, domain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = domain.AgentModeAutoAccept
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 5, 0), domain.HookPreStream)

	assert.Empty(t, conv, "must not inject when reminders are disabled")
	assert.Empty(t, buf.String(), "no stream event when reminders are disabled")
}

// Mode data is populated only at pre_stream; other hook dispatches must never
// fire the on_mode_change reminder even when a change is pending.
func TestModeChangeReminder_OtherHooksDoNotFire(t *testing.T) {
	svc := modeChangeSvc(true, domain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = domain.AgentModeStandard
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 2, 0), domain.HookPostStream)

	for _, m := range conv {
		content, _ := m.Content.AsMessageContent0()
		assert.False(t, strings.Contains(content, "mode has changed"), "mode-change reminder must not fire outside pre_stream")
	}
	assert.Equal(t, domain.AgentModeStandard, svc.lastStreamedMode, "tracking only advances at pre_stream")
}
