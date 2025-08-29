package services

import (
	"fmt"
	"testing"
	"time"

	"github.com/inference-gateway/cli/tests/mocks/generated"
)

func TestToolExecutionOrchestrator_shouldSkipToolExecution(t *testing.T) {
	tests := []struct {
		name                      string
		toolName                  string
		configService             *generated.FakeConfigService
		setupConfigServiceReturns func(*generated.FakeConfigService)
		expected                  bool
	}{
		{
			name:     "nil config service - a2a tool should be skipped (fallback behavior)",
			toolName: "a2a_connect",
			expected: true,
		},
		{
			name:     "nil config service - mcp tool should be skipped (fallback behavior)",
			toolName: "mcp_list_tools",
			expected: true,
		},
		{
			name:     "nil config service - regular tool should not be skipped",
			toolName: "Read",
			expected: false,
		},
		{
			name:          "a2a tool with skip enabled should be skipped",
			toolName:      "a2a_connect",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
			},
			expected: true,
		},
		{
			name:          "a2a tool should always be skipped regardless of config",
			toolName:      "a2a_send_message",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(false)
			},
			expected: true,
		},
		{
			name:          "mcp tool with skip enabled should be skipped",
			toolName:      "mcp_list_tools",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: true,
		},
		{
			name:          "mcp tool should always be skipped regardless of config",
			toolName:      "mcp_execute_command",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipMCPToolOnClientReturns(false)
			},
			expected: true,
		},
		{
			name:          "regular tool should not be skipped regardless of config",
			toolName:      "Read",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "bash tool should not be skipped regardless of config",
			toolName:      "Bash",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "tool with a2a in middle should not be skipped",
			toolName:      "some_a2a_tool",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "tool with mcp in middle should not be skipped",
			toolName:      "some_mcp_tool",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "empty tool name should not be skipped",
			toolName:      "",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "tool starting with A2A caps should not be skipped",
			toolName:      "A2A_tool",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipA2AToolOnClientReturns(true)
			},
			expected: false,
		},
		{
			name:          "tool starting with MCP caps should not be skipped",
			toolName:      "MCP_tool",
			configService: &generated.FakeConfigService{},
			setupConfigServiceReturns: func(fake *generated.FakeConfigService) {
				fake.ShouldSkipMCPToolOnClientReturns(true)
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var teo *ToolExecutionOrchestrator
			if tt.configService != nil {
				teo = &ToolExecutionOrchestrator{
					configService: tt.configService,
				}
				if tt.setupConfigServiceReturns != nil {
					tt.setupConfigServiceReturns(tt.configService)
				}
			} else {
				teo = &ToolExecutionOrchestrator{}
			}

			result := teo.shouldSkipToolExecution(tt.toolName)
			if result != tt.expected {
				t.Errorf("shouldSkipToolExecution(%q) = %v, expected %v", tt.toolName, result, tt.expected)
			}
		})
	}
}

func TestToolExecutionOrchestrator_createSkippedToolResult(t *testing.T) {
	tests := []struct {
		name                 string
		toolName             string
		args                 map[string]any
		expectedToolType     string
		expectedSuccess      bool
		expectedDataType     string
		expectedMetadataType string
	}{
		{
			name:     "a2a tool creates correct result",
			toolName: "a2a_connect",
			args: map[string]any{
				"endpoint": "https://example.com",
				"timeout":  30,
			},
			expectedToolType:     "A2A",
			expectedSuccess:      true,
			expectedDataType:     "A2A",
			expectedMetadataType: "A2A",
		},
		{
			name:     "mcp tool creates correct result",
			toolName: "mcp_list_resources",
			args: map[string]any{
				"filter": "active",
				"limit":  100,
			},
			expectedToolType:     "MCP",
			expectedSuccess:      true,
			expectedDataType:     "MCP",
			expectedMetadataType: "MCP",
		},
		{
			name:                 "a2a tool with empty args",
			toolName:             "a2a_ping",
			args:                 map[string]any{},
			expectedToolType:     "A2A",
			expectedSuccess:      true,
			expectedDataType:     "A2A",
			expectedMetadataType: "A2A",
		},
		{
			name:                 "mcp tool with nil args",
			toolName:             "mcp_status",
			args:                 nil,
			expectedToolType:     "MCP",
			expectedSuccess:      true,
			expectedDataType:     "MCP",
			expectedMetadataType: "MCP",
		},
	}

	teo := &ToolExecutionOrchestrator{}
	duration := 50 * time.Millisecond

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := teo.createSkippedToolResult(tt.toolName, tt.args, duration)

			if result == nil {
				t.Fatal("createSkippedToolResult returned nil")
			}

			if result.ToolName != tt.toolName {
				t.Errorf("ToolName = %q, expected %q", result.ToolName, tt.toolName)
			}

			if result.Success != tt.expectedSuccess {
				t.Errorf("Success = %v, expected %v", result.Success, tt.expectedSuccess)
			}

			if result.Duration != duration {
				t.Errorf("Duration = %v, expected %v", result.Duration, duration)
			}

			if len(result.Arguments) != len(tt.args) {
				t.Errorf("Arguments length = %d, expected %d", len(result.Arguments), len(tt.args))
			}

			dataStr, ok := result.Data.(string)
			if !ok {
				t.Fatal("Data is not string")
			}

			expectedDataStr := fmt.Sprintf("Executed on Gateway (%s)", tt.expectedDataType)
			if dataStr != expectedDataStr {
				t.Errorf("Data = %q, expected %q", dataStr, expectedDataStr)
			}
		})
	}
}
