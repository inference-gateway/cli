package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
)

// MockA2AService implements domain.A2AService for testing
type MockA2AService struct {
	agents []domain.A2AAgent
	err    error
}

func (m *MockA2AService) ListAgents(ctx context.Context) ([]domain.A2AAgent, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.agents, nil
}

func TestA2ACommand_GetMethods(t *testing.T) {
	service := &MockA2AService{}
	cmd := NewA2ACommand(service)

	if cmd.GetName() != "a2a" {
		t.Errorf("Expected name 'a2a', got %q", cmd.GetName())
	}

	if cmd.GetDescription() != "Show A2A agents" {
		t.Errorf("Expected description 'Show A2A agents', got %q", cmd.GetDescription())
	}

	if cmd.GetUsage() != "/a2a" {
		t.Errorf("Expected usage '/a2a', got %q", cmd.GetUsage())
	}
}

func TestA2ACommand_CanExecute(t *testing.T) {
	service := &MockA2AService{}
	cmd := NewA2ACommand(service)

	tests := []struct {
		name     string
		args     []string
		expected bool
	}{
		{"no args", []string{}, true},
		{"one arg", []string{"arg1"}, false},
		{"multiple args", []string{"arg1", "arg2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cmd.CanExecute(tt.args)
			if result != tt.expected {
				t.Errorf("CanExecute(%v) = %v, expected %v", tt.args, result, tt.expected)
			}
		})
	}
}

func TestA2ACommand_Execute_Success(t *testing.T) {
	agents := []domain.A2AAgent{
		{
			ID:       "agent-1",
			Name:     "Test Agent 1",
			Status:   domain.A2AAgentStatusAvailable,
			Endpoint: "http://agent1.example.com",
			Version:  "1.0.0",
		},
		{
			ID:       "agent-2",
			Name:     "Test Agent 2",
			Status:   domain.A2AAgentStatusDegraded,
			Endpoint: "http://agent2.example.com",
			Version:  "1.1.0",
		},
		{
			ID:     "agent-3",
			Name:   "Test Agent 3",
			Status: domain.A2AAgentStatusUnknown,
		},
	}

	service := &MockA2AService{agents: agents}
	cmd := NewA2ACommand(service)

	result, err := cmd.Execute(context.Background(), []string{})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected command to succeed")
	}

	output := result.Output
	if !strings.Contains(output, "A2A Agents: 3 connected") {
		t.Errorf("Expected output to contain '3 connected', got: %s", output)
	}

	for _, agent := range agents {
		if !strings.Contains(output, agent.Name) {
			t.Errorf("Expected output to contain agent name %q, got: %s", agent.Name, output)
		}
		if !strings.Contains(output, agent.ID) {
			t.Errorf("Expected output to contain agent ID %q, got: %s", agent.ID, output)
		}
	}

	if !strings.Contains(output, "✅") {
		t.Errorf("Expected output to contain available status indicator ✅, got: %s", output)
	}
	if !strings.Contains(output, "⚠️") {
		t.Errorf("Expected output to contain degraded status indicator ⚠️, got: %s", output)
	}
	if !strings.Contains(output, "❓") {
		t.Errorf("Expected output to contain unknown status indicator ❓, got: %s", output)
	}
}

func TestA2ACommand_Execute_EmptyAgents(t *testing.T) {
	service := &MockA2AService{agents: []domain.A2AAgent{}}
	cmd := NewA2ACommand(service)

	result, err := cmd.Execute(context.Background(), []string{})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Expected command to succeed")
	}

	output := result.Output
	if !strings.Contains(output, "A2A Agents: 0 connected") {
		t.Errorf("Expected output to contain '0 connected', got: %s", output)
	}

	if !strings.Contains(output, "No A2A agents are currently connected") {
		t.Errorf("Expected output to contain no agents message, got: %s", output)
	}
}

func TestA2ACommand_Execute_Error(t *testing.T) {
	service := &MockA2AService{err: errors.New("connection failed")}
	cmd := NewA2ACommand(service)

	result, err := cmd.Execute(context.Background(), []string{})

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if result.Success {
		t.Error("Expected command to fail")
	}

	output := result.Output
	if !strings.Contains(output, "Failed to fetch A2A agents") {
		t.Errorf("Expected output to contain error message, got: %s", output)
	}

	if !strings.Contains(output, "connection failed") {
		t.Errorf("Expected output to contain specific error, got: %s", output)
	}
}

func TestA2ACommand_Execute_StatusFormatting(t *testing.T) {
	tests := []struct {
		name           string
		status         domain.A2AAgentStatus
		expectedEmoji  string
		expectedStatus string
	}{
		{
			name:           "available status",
			status:         domain.A2AAgentStatusAvailable,
			expectedEmoji:  "✅",
			expectedStatus: "available",
		},
		{
			name:           "degraded status",
			status:         domain.A2AAgentStatusDegraded,
			expectedEmoji:  "⚠️",
			expectedStatus: "degraded",
		},
		{
			name:           "unknown status",
			status:         domain.A2AAgentStatusUnknown,
			expectedEmoji:  "❓",
			expectedStatus: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := []domain.A2AAgent{
				{
					ID:     "test-agent",
					Name:   "Test Agent",
					Status: tt.status,
				},
			}

			service := &MockA2AService{agents: agents}
			cmd := NewA2ACommand(service)

			result, err := cmd.Execute(context.Background(), []string{})

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !result.Success {
				t.Error("Expected command to succeed")
			}

			output := result.Output
			if !strings.Contains(output, tt.expectedEmoji) {
				t.Errorf("Expected output to contain emoji %q, got: %s", tt.expectedEmoji, output)
			}

			if !strings.Contains(output, tt.expectedStatus) {
				t.Errorf("Expected output to contain status %q, got: %s", tt.expectedStatus, output)
			}
		})
	}
}
