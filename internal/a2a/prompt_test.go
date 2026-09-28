package a2a

import (
	"testing"

	assert "github.com/stretchr/testify/assert"

	a2amocks "github.com/inference-gateway/cli/tests/mocks/a2a"

	a2adomain "github.com/inference-gateway/cli/internal/a2a/domain"
)

func TestAgentsPromptSection(t *testing.T) {
	agentsService := func(urls []string) *a2amocks.FakeAgentCardService {
		fake := &a2amocks.FakeAgentCardService{}
		fake.GetConfiguredAgentsReturns(urls)
		return fake
	}

	tests := []struct {
		name          string
		agents        a2adomain.AgentCardService
		expectedParts []string
	}{
		{name: "no service", agents: nil},
		{name: "no agents", agents: agentsService([]string{})},
		{
			name:          "configured agents",
			agents:        agentsService([]string{"http://agent1.local", "http://agent2.local"}),
			expectedParts: []string{"Available A2A Agents:", "- http://agent1.local\n", "- http://agent2.local\n", "A2A_SubmitTask tool"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			section := AgentsPromptSection(tt.agents)
			if tt.expectedParts == nil {
				assert.Empty(t, section)
				return
			}
			for _, part := range tt.expectedParts {
				assert.Contains(t, section, part)
			}
		})
	}
}
