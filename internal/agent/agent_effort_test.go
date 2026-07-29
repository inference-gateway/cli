package agent

import (
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"
)

func TestSetReasoningEffort(t *testing.T) {
	s := &AgentServiceImpl{}

	require.NoError(t, s.SetReasoningEffort("max"))
	assert.Equal(t, "max", s.GetReasoningEffort())

	require.Error(t, s.SetReasoningEffort("bogus"))
	assert.Equal(t, "max", s.GetReasoningEffort(), "invalid level must not clobber the current one")

	require.NoError(t, s.SetReasoningEffort(""))
	assert.Empty(t, s.GetReasoningEffort(), "empty string resets to the provider default")
}

func TestReasoningEffortOptionFor(t *testing.T) {
	tests := []struct {
		name   string
		effort string
		model  string
		want   string
	}{
		{"unset yields no option", "", "anthropic/claude-opus-4-8", ""},
		{"anthropic keeps max", "max", "anthropic/claude-opus-4-8", "max"},
		{"anthropic keeps xhigh", "xhigh", "anthropic/claude-opus-4-8", "xhigh"},
		{"anthropic keeps minimal for the adapter to map", "minimal", "anthropic/claude-opus-4-8", "minimal"},
		{"openai clamps max to high", "max", "openai/gpt-5", "high"},
		{"openai clamps xhigh to high", "xhigh", "openai/gpt-5", "high"},
		{"openai keeps medium", "medium", "openai/gpt-5", "medium"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &AgentServiceImpl{}
			require.NoError(t, s.SetReasoningEffort(tt.effort))

			got := s.reasoningEffortOptionFor(tt.model)
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			assert.Equal(t, tt.want, string(*got))
		})
	}
}
