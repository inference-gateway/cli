package agent

import (
	"bytes"
	"encoding/json"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	streamevent "github.com/inference-gateway/cli/internal/streamevent"
	domainmocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

// remindersConfig builds a Config whose SystemReminders drive the agent's
// reminder injection (the config value implements domain.SystemReminderProvider).
func remindersConfig(enabled bool, reminders ...config.ReminderConfig) *config.Config {
	return &config.Config{
		Reminders: config.RemindersConfig{
			Enabled:   enabled,
			Reminders: reminders,
		},
	}
}

// withDebugStreamWriter wires a buffer and forces the streamevent debug gate on
// for the lifetime of t.
func withDebugStreamWriter(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	t.Cleanup(streamevent.SetWriter(&buf))
	t.Cleanup(streamevent.SetDebugEnabledForTest(true))
	return &buf
}

func newReminderAgentCtx(conv *[]sdk.Message, turns, maxTurns int) *domain.AgentContext {
	return &domain.AgentContext{Conversation: conv, Turns: turns, MaxTurns: maxTurns}
}

func reminder(name, text string, hook domain.HookPoint, trigger config.ReminderTrigger, interval int) config.ReminderConfig {
	return config.ReminderConfig{Name: name, Text: text, Hook: hook, Trigger: trigger, Interval: interval}
}

func TestInjectDueReminders_AppendsHiddenMessageAndEmits(t *testing.T) {
	cfg := remindersConfig(true, reminder("todo", "remember to push", domain.HookPreStream, config.ReminderTriggerInterval, 2))
	svc := &AgentServiceImpl{config: cfg}
	svc.sessionTurns.Store(4)
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	agentCtx := newReminderAgentCtx(&conv, 4, 0)
	svc.injectDueReminders(agentCtx, domain.HookPreStream)

	require.Len(t, conv, 1, "reminder must be appended to conversation")
	assert.Equal(t, sdk.User, conv[0].Role)

	var event map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event))
	assert.Equal(t, "user", event["role"])
	assert.Equal(t, "remember to push", event["content"])
	assert.Equal(t, true, event["hidden"])
	assert.Equal(t, "system_reminder", event["kind"])
	assert.Equal(t, "pre_stream", event["hook"], "event must be tagged with the hook point")
	assert.Equal(t, "todo", event["name"], "event must be tagged with the reminder name")
	assert.EqualValues(t, 4, event["turn"])
	assert.EqualValues(t, 4, event["session_turn"], "event must carry the cumulative session turn")
	assert.NotEmpty(t, event["timestamp"])
}

// Gating cases: disabled config, wrong hook, stacked reminders on one hook,
// pending tool results, and the debug stream gate being off.
func TestInjectDueReminders_InjectionGating(t *testing.T) {
	toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
	tests := []struct {
		name         string
		cfg          *config.Config
		initialConv  []sdk.Message
		hook         domain.HookPoint
		turns        int
		debugOn      bool
		wantConvLen  int
		wantBufEmpty bool
	}{
		{
			name:         "no-op when disabled",
			cfg:          remindersConfig(false, reminder("todo", "ignored", domain.HookPreStream, config.ReminderTriggerInterval, 2)),
			hook:         domain.HookPreStream,
			turns:        4,
			debugOn:      true,
			wantConvLen:  0,
			wantBufEmpty: true,
		},
		{
			name:        "no-op on wrong hook",
			cfg:         remindersConfig(true, reminder("todo", "x", domain.HookPostTool, config.ReminderTriggerAlways, 0)),
			hook:        domain.HookPreStream,
			turns:       1,
			debugOn:     true,
			wantConvLen: 0,
		},
		{
			name: "stacking injects all reminders on the hook",
			cfg: remindersConfig(true,
				reminder("todo", "t", domain.HookPreStream, config.ReminderTriggerAlways, 0),
				reminder("memory", "m", domain.HookPreStream, config.ReminderTriggerAlways, 0),
			),
			hook:        domain.HookPreStream,
			turns:       1,
			debugOn:     true,
			wantConvLen: 2,
		},
		{
			name: "skips while awaiting tool results",
			cfg:  remindersConfig(true, reminder("note", "n", domain.HookPreTool, config.ReminderTriggerAlways, 0)),
			initialConv: []sdk.Message{
				{Role: sdk.User, Content: sdk.NewMessageContent("do it")},
				{Role: sdk.Assistant, ToolCalls: &toolCalls},
			},
			hook:        domain.HookPreTool,
			turns:       1,
			debugOn:     true,
			wantConvLen: 2,
		},
		{
			name:         "debug gate off appends but does not stream",
			cfg:          remindersConfig(true, reminder("todo", "x", domain.HookPreStream, config.ReminderTriggerAlways, 0)),
			hook:         domain.HookPreStream,
			turns:        1,
			debugOn:      false,
			wantConvLen:  1,
			wantBufEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &AgentServiceImpl{config: tt.cfg}
			var buf bytes.Buffer
			t.Cleanup(streamevent.SetWriter(&buf))
			t.Cleanup(streamevent.SetDebugEnabledForTest(tt.debugOn))

			conv := tt.initialConv
			if conv == nil {
				conv = []sdk.Message{}
			}
			svc.injectDueReminders(newReminderAgentCtx(&conv, tt.turns, 0), tt.hook)

			require.Len(t, conv, tt.wantConvLen)
			if tt.wantBufEmpty {
				assert.Empty(t, buf.String())
			}
		})
	}
}

// A `once` reminder fires the first time its hook dispatches and is suppressed
// afterwards via the session-scoped fired-set on the service.
func TestInjectDueReminders_OnceSuppressedAfterFiring(t *testing.T) {
	cfg := remindersConfig(true, reminder("memory", "load memory", domain.HookPreSession, config.ReminderTriggerOnce, 0))
	svc := &AgentServiceImpl{config: cfg}

	conv := []sdk.Message{}
	agentCtx := newReminderAgentCtx(&conv, 1, 0)

	svc.injectDueReminders(agentCtx, domain.HookPreSession)
	require.Len(t, conv, 1, "once reminder fires the first time")

	agentCtx.Turns = 2
	svc.injectDueReminders(agentCtx, domain.HookPreSession)
	require.Len(t, conv, 1, "once reminder must not fire again")
}

// The agent depends on the SystemReminderProvider interface, not the concrete
// config: the wired provider is consulted with the live query (hook, per-request
// turn, cumulative session turn, max turns, tool-failed) and its result injected.
func TestInjectDueReminders_WiredProviderQuery(t *testing.T) {
	tests := []struct {
		name            string
		providerReturns []domain.SystemReminder
		turns           int
		maxTurns        int
		sessionTurns    int64
		lastToolFailed  bool
		wantContent     string
	}{
		{
			name:            "query fields and returned reminder injected",
			providerReturns: []domain.SystemReminder{{Name: "fake", Text: "from provider"}},
			turns:           7,
			maxTurns:        12,
			sessionTurns:    9,
			wantContent:     "from provider",
		},
		{
			name:           "AgentContext.LastToolFailed plumbed into query",
			turns:          1,
			lastToolFailed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &domainmocks.FakeSystemReminderProvider{}
			fake.RemindersDueReturns(tt.providerReturns)
			svc := &AgentServiceImpl{reminderProvider: fake}
			svc.sessionTurns.Store(tt.sessionTurns)

			conv := []sdk.Message{}
			agentCtx := newReminderAgentCtx(&conv, tt.turns, tt.maxTurns)
			agentCtx.LastToolFailed = tt.lastToolFailed
			svc.injectDueReminders(agentCtx, domain.HookPostTool)

			require.Equal(t, 1, fake.RemindersDueCallCount())
			q := fake.RemindersDueArgsForCall(0)
			assert.Equal(t, domain.HookPostTool, q.Hook)
			assert.Equal(t, tt.turns, q.Turn, "per-request turn comes from AgentContext.Turns")
			assert.Equal(t, int(tt.sessionTurns), q.SessionTurn, "session turn comes from the cumulative counter")
			assert.Equal(t, tt.maxTurns, q.MaxTurns)
			assert.Equal(t, tt.lastToolFailed, q.ToolFailed, "ToolFailed must reflect AgentContext.LastToolFailed")

			if tt.wantContent != "" {
				require.Len(t, conv, 1)
				content, _ := conv[0].Content.AsMessageContent0()
				assert.Equal(t, tt.wantContent, content)
			}
		})
	}
}

// Reminder cadence is session-scoped: an `interval` reminder fires on the Nth
// cumulative conversational turn even though each user message runs as a fresh
// AgentContext whose per-request Turns resets to 1.
func TestInjectDueReminders_IntervalCountsAcrossSeparateRequests(t *testing.T) {
	cfg := remindersConfig(true, reminder("todo", "nudge", domain.HookPreStream, config.ReminderTriggerInterval, 4))
	svc := &AgentServiceImpl{config: cfg}

	var firedAt []int
	for msg := 1; msg <= 8; msg++ {
		conv := []sdk.Message{}
		agentCtx := newReminderAgentCtx(&conv, 1, 50)
		svc.sessionTurns.Add(1)
		svc.injectDueReminders(agentCtx, domain.HookPreStream)
		if len(conv) > 0 {
			firedAt = append(firedAt, int(svc.sessionTurns.Load()))
		}
	}

	assert.Equal(t, []int{4, 8}, firedAt,
		"interval:4 must fire on cumulative turns 4 and 8 across separate single-turn requests")
}
