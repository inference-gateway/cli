package shortcuts

import (
	"context"
	"errors"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
)

func TestEffortShortcut_ShowCurrent(t *testing.T) {
	agent := &agentdomainmocks.FakeAgentService{}
	agent.GetReasoningEffortReturns("xhigh")
	shortcut := NewEffortShortcut(agent, &mockModelService{currentModel: "anthropic/claude-opus-4-8"})

	result, err := shortcut.Execute(context.Background(), nil)

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.Output, "xhigh")
	assert.Contains(t, result.Output, "max", "available levels must be listed")
}

func TestEffortShortcut_ShowDefault(t *testing.T) {
	shortcut := NewEffortShortcut(&agentdomainmocks.FakeAgentService{}, &mockModelService{})

	result, err := shortcut.Execute(context.Background(), nil)

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.Output, "low (default)")
}

func TestEffortShortcut_SetOnAnthropicModel(t *testing.T) {
	agent := &agentdomainmocks.FakeAgentService{}
	shortcut := NewEffortShortcut(agent, &mockModelService{currentModel: "anthropic/claude-opus-4-8"})

	result, err := shortcut.Execute(context.Background(), []string{"MAX"})

	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Contains(t, result.Output, "max")
	require.Equal(t, 1, agent.SetReasoningEffortCallCount())
	assert.Equal(t, "max", agent.SetReasoningEffortArgsForCall(0), "level is lowercased before setting")
}

func TestEffortShortcut_RejectsNonAnthropicModel(t *testing.T) {
	agent := &agentdomainmocks.FakeAgentService{}
	shortcut := NewEffortShortcut(agent, &mockModelService{currentModel: "openai/gpt-5"})

	result, err := shortcut.Execute(context.Background(), []string{"high"})

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "not supported")
	assert.Zero(t, agent.SetReasoningEffortCallCount())
}

func TestEffortShortcut_InvalidLevel(t *testing.T) {
	agent := &agentdomainmocks.FakeAgentService{}
	agent.SetReasoningEffortReturns(errors.New(`invalid reasoning effort "bogus"`))
	shortcut := NewEffortShortcut(agent, &mockModelService{currentModel: "anthropic/claude-opus-4-8"})

	result, err := shortcut.Execute(context.Background(), []string{"bogus"})

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Output, "invalid reasoning effort")
}

func TestEffortShortcut_CanExecute(t *testing.T) {
	shortcut := NewEffortShortcut(&agentdomainmocks.FakeAgentService{}, &mockModelService{})

	assert.True(t, shortcut.CanExecute(nil))
	assert.True(t, shortcut.CanExecute([]string{"high"}))
	assert.False(t, shortcut.CanExecute([]string{"high", "extra"}))
}
