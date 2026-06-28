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
		Prompts: config.PromptsConfig{
			Agent: config.PromptsAgentConfig{
				SystemReminders: config.PromptsAgentRemindersConfig{
					Enabled:   enabled,
					Reminders: reminders,
				},
			},
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
	assert.NotEmpty(t, event["timestamp"])
}

func TestInjectDueReminders_NoOpWhenDisabled(t *testing.T) {
	cfg := remindersConfig(false, reminder("todo", "ignored", domain.HookPreStream, config.ReminderTriggerInterval, 2))
	svc := &AgentServiceImpl{config: cfg}
	buf := withDebugStreamWriter(t)

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 4, 0), domain.HookPreStream)

	assert.Empty(t, conv)
	assert.Empty(t, buf.String())
}

func TestInjectDueReminders_NoOpOnWrongHook(t *testing.T) {
	cfg := remindersConfig(true, reminder("todo", "x", domain.HookPostTool, config.ReminderTriggerAlways, 0))
	svc := &AgentServiceImpl{config: cfg}

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreStream)
	assert.Empty(t, conv, "a post_tool reminder must not fire at pre_stream")
}

// Multiple reminders configured on the same hook all inject in one dispatch.
func TestInjectDueReminders_Stacking(t *testing.T) {
	cfg := remindersConfig(true,
		reminder("todo", "t", domain.HookPreStream, config.ReminderTriggerAlways, 0),
		reminder("memory", "m", domain.HookPreStream, config.ReminderTriggerAlways, 0),
	)
	svc := &AgentServiceImpl{config: cfg}

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreStream)
	require.Len(t, conv, 2, "both reminders on the hook should inject")
}

// A `once` reminder fires the first time its hook dispatches and is suppressed
// afterwards via the per-run fired-set on the AgentContext.
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

// A reminder is a user message; it must not be injected between an assistant
// turn's tool_calls and its results (would orphan the calls). The mid-turn
// hooks (post_stream/pre_tool) hit this state when the model requested tools.
func TestInjectDueReminders_SkipsWhenAwaitingToolResults(t *testing.T) {
	cfg := remindersConfig(true, reminder("note", "n", domain.HookPreTool, config.ReminderTriggerAlways, 0))
	svc := &AgentServiceImpl{config: cfg}

	toolCalls := []sdk.ChatCompletionMessageToolCall{{ID: "call_1"}}
	conv := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent("do it")},
		{Role: sdk.Assistant, ToolCalls: &toolCalls},
	}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreTool)
	assert.Len(t, conv, 2, "must not inject while awaiting tool results")
}

// The agent depends on the SystemReminderProvider interface, not the concrete
// config: a wired provider is consulted with the live (hook, turn, maxTurns)
// and whatever it returns is injected.
func TestInjectDueReminders_UsesWiredProvider(t *testing.T) {
	fake := &domainmocks.FakeSystemReminderProvider{}
	fake.RemindersDueReturns([]domain.SystemReminder{{Name: "fake", Text: "from provider"}})
	svc := &AgentServiceImpl{reminderProvider: fake}

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 7, 12), domain.HookPostTool)

	require.Equal(t, 1, fake.RemindersDueCallCount())
	hook, turn, maxTurns, _ := fake.RemindersDueArgsForCall(0)
	assert.Equal(t, domain.HookPostTool, hook)
	assert.Equal(t, 7, turn)
	assert.Equal(t, 12, maxTurns)

	require.Len(t, conv, 1)
	content, _ := conv[0].Content.AsMessageContent0()
	assert.Equal(t, "from provider", content)
}

func TestInjectDueReminders_DebugGateOff_AppendsButNoStream(t *testing.T) {
	cfg := remindersConfig(true, reminder("todo", "x", domain.HookPreStream, config.ReminderTriggerAlways, 0))
	svc := &AgentServiceImpl{config: cfg}

	var buf bytes.Buffer
	t.Cleanup(streamevent.SetWriter(&buf))
	t.Cleanup(streamevent.SetDebugEnabledForTest(false))

	conv := []sdk.Message{}
	svc.injectDueReminders(newReminderAgentCtx(&conv, 1, 0), domain.HookPreStream)

	require.Len(t, conv, 1, "conversation still gets the reminder")
	assert.Empty(t, buf.String(), "no stream event when the debug gate is off")
}
