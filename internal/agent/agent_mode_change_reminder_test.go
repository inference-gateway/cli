package agent

import (
	"bytes"
	"encoding/json"
	statemanager "github.com/inference-gateway/cli/internal/presentation/tui/statemanager"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"
	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convmocks "github.com/inference-gateway/cli/tests/mocks/conversation"
)

// modeChangeSvc builds an agent service backed by the real default reminders
// config (which carries the on_mode_change entry) and a fake state manager
// reporting the given live mode.
func modeChangeSvc(enabled bool, liveMode agentdomain.AgentMode) *AgentServiceImpl {
	sm := statemanager.NewStateManager(false)
	sm.SetAgentMode(liveMode)
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	cfg.Reminders.Enabled = enabled
	return &AgentServiceImpl{stateManager: sm, config: cfg}
}

// assertNoModeChangeContent verifies no conversation message carries mode-change
// reminder content or a raw template placeholder.
func assertNoModeChangeContent(t *testing.T, conv []sdk.Message) {
	t.Helper()
	for _, m := range conv {
		content, _ := m.Content.AsMessageContent0()
		assert.NotContains(t, content, "{prev_mode}", "raw template must never leak")
		assert.False(t, strings.Contains(content, "mode has changed"), "mode-change reminder must not fire here")
	}
}

// No-injection cases: seeding turn, same-mode steady state, missing state
// manager, pending tool results, disabled reminders, non-pre_stream hooks.
func TestModeChangeReminder_NoInjectionCases(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
	tests := []struct {
		name                string
		svc                 func() *AgentServiceImpl
		initialConv         []sdk.Message
		hook                agentdomain.HookPoint
		dispatches          int
		wantBufEmpty        bool
		wantModeInitialized *bool
		wantLastMode        *agentdomain.AgentMode
	}{
		{
			name:                "first turn seeds without injecting",
			svc:                 func() *AgentServiceImpl { return modeChangeSvc(true, agentdomain.AgentModePlan) },
			hook:                agentdomain.HookPreStream,
			dispatches:          1,
			wantBufEmpty:        true,
			wantModeInitialized: ptr(true),
			wantLastMode:        ptr(agentdomain.AgentModePlan),
		},
		{
			name:       "same mode across turns injects nothing",
			svc:        func() *AgentServiceImpl { return modeChangeSvc(true, agentdomain.AgentModeStandard) },
			hook:       agentdomain.HookPreStream,
			dispatches: 3,
		},
		{
			name: "nil state manager never seeds or injects",
			svc: func() *AgentServiceImpl {
				return &AgentServiceImpl{config: &config.Config{Reminders: *config.DefaultRemindersConfig()}}
			},
			hook:                agentdomain.HookPreStream,
			dispatches:          1,
			wantBufEmpty:        true,
			wantModeInitialized: ptr(false),
		},
		{
			name: "skips while awaiting tool results without advancing tracking",
			svc: func() *AgentServiceImpl {
				svc := modeChangeSvc(true, agentdomain.AgentModePlan)
				svc.modeInitialized = true
				svc.lastStreamedMode = agentdomain.AgentModeStandard
				return svc
			},
			initialConv: []sdk.Message{
				{Role: sdk.User, Content: sdk.NewMessageContent("go")},
				{Role: sdk.Assistant, ToolCalls: &toolCalls},
			},
			hook:         agentdomain.HookPreStream,
			dispatches:   1,
			wantLastMode: ptr(agentdomain.AgentModeStandard),
		},
		{
			name: "disabled reminders gate the mode-change entry too",
			svc: func() *AgentServiceImpl {
				svc := modeChangeSvc(false, agentdomain.AgentModePlan)
				svc.modeInitialized = true
				svc.lastStreamedMode = agentdomain.AgentModeAutoAccept
				return svc
			},
			hook:         agentdomain.HookPreStream,
			dispatches:   1,
			wantBufEmpty: true,
		},
		{
			name: "other hooks never fire a pending change",
			svc: func() *AgentServiceImpl {
				svc := modeChangeSvc(true, agentdomain.AgentModePlan)
				svc.modeInitialized = true
				svc.lastStreamedMode = agentdomain.AgentModeStandard
				return svc
			},
			hook:         agentdomain.HookPostStream,
			dispatches:   1,
			wantLastMode: ptr(agentdomain.AgentModeStandard),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := tt.svc()
			buf := withDebugStreamWriter(t)

			conv := tt.initialConv
			if conv == nil {
				conv = []sdk.Message{}
			}
			initialLen := len(conv)
			ctx := newReminderAgentCtx(&conv, 1, 0)
			for range tt.dispatches {
				svc.injectDueReminders(ctx, tt.hook)
			}

			assert.Len(t, conv, initialLen, "no message may be injected")
			assertNoModeChangeContent(t, conv)
			if tt.wantBufEmpty {
				assert.Empty(t, buf.String(), "no stream event expected")
			}
			if tt.wantModeInitialized != nil {
				assert.Equal(t, *tt.wantModeInitialized, svc.modeInitialized)
			}
			if tt.wantLastMode != nil {
				assert.Equal(t, *tt.wantLastMode, svc.lastStreamedMode)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

// The core acceptance criterion: switching Auto -> Plan mid-session injects one
// hidden user message carrying the formatted new-mode guidance and emits a
// system_reminder stream event tagged with the reminder name.
func TestModeChangeReminder_AutoToPlanInjectsAndEmits(t *testing.T) {
	svc := modeChangeSvc(true, agentdomain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = agentdomain.AgentModeAutoAccept
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 5, 0), agentdomain.HookPreStream)

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

	assert.Equal(t, agentdomain.AgentModePlan, svc.lastStreamedMode, "lastStreamedMode advances to the new mode")
}

// The persisted ConversationEntry must be flagged Hidden so the mode-change
// reminder never surfaces as a visible user message in history.
func TestModeChangeReminder_PersistsHiddenViaRepo(t *testing.T) {
	svc := modeChangeSvc(true, agentdomain.AgentModePlan)
	svc.modeInitialized = true
	svc.lastStreamedMode = agentdomain.AgentModeStandard
	repo := &convmocks.FakeConversationRepository{}
	repo.AddMessageReturns(nil)
	svc.conversationRepo = repo
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 2, 0), agentdomain.HookPreStream)

	require.Equal(t, 1, repo.AddMessageCallCount(), "reminder must be persisted once")
	entry := repo.AddMessageArgsForCall(0)
	assert.True(t, entry.Hidden, "persisted entry must be hidden")
	assert.Equal(t, sdk.User, entry.Message.Role)
}

// After a change is recorded, the new mode becomes the baseline; a later turn
// in that same mode must not re-inject.
func TestModeChangeReminder_BaselineAdvancesAfterChange(t *testing.T) {
	sm := statemanager.NewStateManager(false)
	cfg := &config.Config{Reminders: *config.DefaultRemindersConfig()}
	svc := &AgentServiceImpl{stateManager: sm, config: cfg}
	withDebugStreamWriter(t)

	conv := []sdk.Message{}
	ctx := newReminderAgentCtx(&conv, 1, 0)

	sm.SetAgentMode(agentdomain.AgentModeAutoAccept)
	svc.injectDueReminders(ctx, agentdomain.HookPreStream)
	require.Empty(t, conv, "turn 1 seeds")

	sm.SetAgentMode(agentdomain.AgentModePlan)
	svc.injectDueReminders(ctx, agentdomain.HookPreStream)
	require.Len(t, conv, 1, "Auto -> Plan injects once")

	svc.injectDueReminders(ctx, agentdomain.HookPreStream)
	require.Len(t, conv, 1, "must not re-inject once the new mode is the baseline")
}
