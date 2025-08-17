package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/inference-gateway/cli/internal/domain"
)

func TestHTTPA2AService_ListAgents(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		statusCode     int
		expectedAgents []domain.A2AAgent
		expectError    bool
	}{
		{
			name:       "successful response with agents",
			statusCode: http.StatusOK,
			serverResponse: `{
				"data": [
					{
						"id": "agent-1",
						"name": "Test Agent 1",
						"status": "available",
						"endpoint": "http://agent1.example.com",
						"version": "1.0.0"
					},
					{
						"id": "agent-2",
						"name": "Test Agent 2",
						"status": "degraded",
						"endpoint": "http://agent2.example.com",
						"version": "1.1.0"
					}
				]
			}`,
			expectedAgents: []domain.A2AAgent{
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
			},
			expectError: false,
		},
		{
			name:       "empty response",
			statusCode: http.StatusOK,
			serverResponse: `{
				"data": []
			}`,
			expectedAgents: []domain.A2AAgent{},
			expectError:    false,
		},
		{
			name:           "server error",
			statusCode:     http.StatusInternalServerError,
			serverResponse: `{"error": "Internal server error"}`,
			expectedAgents: nil,
			expectError:    true,
		},
		{
			name:       "unknown status mapping",
			statusCode: http.StatusOK,
			serverResponse: `{
				"data": [
					{
						"id": "agent-3",
						"name": "Test Agent 3",
						"status": "offline",
						"endpoint": "http://agent3.example.com"
					}
				]
			}`,
			expectedAgents: []domain.A2AAgent{
				{
					ID:       "agent-3",
					Name:     "Test Agent 3",
					Status:   domain.A2AAgentStatusUnknown,
					Endpoint: "http://agent3.example.com",
					Version:  "",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createTestServer(t, tt.statusCode, tt.serverResponse)
			defer server.Close()

			service := NewHTTPA2AService(server.URL, "test-api-key")
			agents, err := service.ListAgents(context.Background())

			validateTestResult(t, tt.expectError, err, agents, tt.expectedAgents)
		})
	}
}

func createTestServer(t *testing.T, statusCode int, response string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/a2a/agents" {
			t.Errorf("Expected path '/v1/a2a/agents', got %q", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("Expected method GET, got %s", r.Method)
		}

		w.WriteHeader(statusCode)
		if _, err := w.Write([]byte(response)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
}

func validateTestResult(t *testing.T, expectError bool, err error, agents []domain.A2AAgent, expectedAgents []domain.A2AAgent) {
	if expectError {
		if err == nil {
			t.Error("Expected error but got none")
		}
		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
		return
	}

	if len(agents) != len(expectedAgents) {
		t.Errorf("Expected %d agents, got %d", len(expectedAgents), len(agents))
		return
	}

	for i, expected := range expectedAgents {
		if i >= len(agents) {
			t.Errorf("Missing agent at index %d", i)
			continue
		}
		validateAgent(t, i, agents[i], expected)
	}
}

func validateAgent(t *testing.T, index int, actual, expected domain.A2AAgent) {
	if actual.ID != expected.ID {
		t.Errorf("Agent %d: expected ID %q, got %q", index, expected.ID, actual.ID)
	}
	if actual.Name != expected.Name {
		t.Errorf("Agent %d: expected name %q, got %q", index, expected.Name, actual.Name)
	}
	if actual.Status != expected.Status {
		t.Errorf("Agent %d: expected status %q, got %q", index, expected.Status, actual.Status)
	}
	if actual.Endpoint != expected.Endpoint {
		t.Errorf("Agent %d: expected endpoint %q, got %q", index, expected.Endpoint, actual.Endpoint)
	}
	if actual.Version != expected.Version {
		t.Errorf("Agent %d: expected version %q, got %q", index, expected.Version, actual.Version)
	}
}

func TestHTTPA2AService_ListAgents_WithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "Bearer test-api-key" {
			t.Errorf("Expected Authorization header 'Bearer test-api-key', got %q", authHeader)
		}

		contentType := r.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("Expected Content-Type 'application/json', got %q", contentType)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"data": []}`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := NewHTTPA2AService(server.URL, "test-api-key")
	_, err := service.ListAgents(context.Background())

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHTTPA2AService_ListAgents_WithoutAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			t.Errorf("Expected no Authorization header, got %q", authHeader)
		}

		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`{"data": []}`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := NewHTTPA2AService(server.URL, "")
	_, err := service.ListAgents(context.Background())

	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestHTTPA2AService_ListAgents_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(`invalid json`)); err != nil {
			t.Errorf("Failed to write response: %v", err)
		}
	}))
	defer server.Close()

	service := NewHTTPA2AService(server.URL, "")
	_, err := service.ListAgents(context.Background())

	if err == nil {
		t.Error("Expected error for invalid JSON but got none")
	}
	if !strings.Contains(err.Error(), "failed to parse response") {
		t.Errorf("Expected error to contain 'failed to parse response', got: %v", err)
	}
}

func TestMapAgentStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected domain.A2AAgentStatus
	}{
		{"available", domain.A2AAgentStatusAvailable},
		{"Available", domain.A2AAgentStatusAvailable},
		{"AVAILABLE", domain.A2AAgentStatusAvailable},
		{"degraded", domain.A2AAgentStatusDegraded},
		{"Degraded", domain.A2AAgentStatusDegraded},
		{"DEGRADED", domain.A2AAgentStatusDegraded},
		{"unknown", domain.A2AAgentStatusUnknown},
		{"offline", domain.A2AAgentStatusUnknown},
		{"invalid", domain.A2AAgentStatusUnknown},
		{"", domain.A2AAgentStatusUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := mapAgentStatus(tt.input)
			if result != tt.expected {
				t.Errorf("mapAgentStatus(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHTTPA2AService_ListAgents_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't respond to simulate timeout
		select {}
	}))
	defer server.Close()

	service := NewHTTPA2AService(server.URL, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately to trigger timeout

	_, err := service.ListAgents(ctx)

	if err == nil {
		t.Error("Expected error for cancelled context but got none")
	}
}
