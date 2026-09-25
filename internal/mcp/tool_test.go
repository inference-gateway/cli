package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"

	mcpmocks "github.com/inference-gateway/cli/tests/mocks/mcp"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	mcpdomain "github.com/inference-gateway/cli/internal/mcp/domain"
)

func TestNewMCPTool(t *testing.T) {
	mcpConfig := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Enabled: true,
			},
		},
	}

	mockClient := &mcpmocks.FakeClient{}

	tool := NewMCPTool(
		"test-server",
		"readFile",
		"Reads a file from the filesystem",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Path to the file",
				},
			},
			"required": []any{"path"},
		},
		mockClient,
		mcpConfig,
	)

	if tool == nil {
		t.Fatal("Expected non-nil tool")
	}

	if tool.serverName != "test-server" {
		t.Errorf("Expected serverName 'test-server', got %s", tool.serverName)
	}

	if tool.toolName != "readFile" {
		t.Errorf("Expected toolName 'readFile', got %s", tool.toolName)
	}

	if tool.description != "Reads a file from the filesystem" {
		t.Errorf("Expected description to match, got %s", tool.description)
	}
}

func TestMCPTool_Definition(t *testing.T) {
	mcpConfig := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Enabled: true,
			},
		},
	}

	mockClient := &mcpmocks.FakeClient{}

	tool := NewMCPTool(
		"test-server",
		"readFile",
		"Reads a file",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type": "string",
				},
			},
		},
		mockClient,
		mcpConfig,
	)

	def := tool.Definition()

	expectedName := "MCP_test-server_readFile"
	if def.Function.Name != expectedName {
		t.Errorf("Expected function name %s, got %s", expectedName, def.Function.Name)
	}

	if def.Type != sdk.Function {
		t.Errorf("Expected type Function, got %v", def.Type)
	}

	if def.Function.Description == nil {
		t.Fatal("Expected non-nil description")
	}

	expectedDescPrefix := "[MCP:test-server]"
	if len(*def.Function.Description) < len(expectedDescPrefix) {
		t.Errorf("Expected description to start with %s", expectedDescPrefix)
	}

	if def.Function.Parameters == nil {
		t.Error("Expected non-nil parameters")
	}
}

func TestMCPTool_Execute_Success(t *testing.T) {
	mcpConfig := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Enabled: true,
			},
		},
	}

	mockClient := &mcpmocks.FakeClient{}

	mockClient.CallToolReturns(mcpdomain.CallResult{Content: "File contents here"}, nil)

	tool := NewMCPTool(
		"test-server",
		"readFile",
		"Reads a file",
		nil,
		mockClient,
		mcpConfig,
	)

	ctx := context.Background()
	args := map[string]any{"path": "/test/file.txt"}

	result, err := tool.Execute(ctx, args)

	if err != nil {
		t.Fatalf("Execute() returned error: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if !result.Success {
		t.Error("Expected successful execution")
	}

	if result.ToolName != "MCP_test-server_readFile" {
		t.Errorf("Expected tool name 'MCP_test-server_readFile', got %s", result.ToolName)
	}

	mcpData, ok := result.Data.(*mcpdomain.ToolResult)
	if !ok {
		t.Fatal("Expected result.Data to be *mcpdomain.ToolResult")
	}

	if mcpData.ServerName != "test-server" {
		t.Errorf("Expected server name 'test-server', got %s", mcpData.ServerName)
	}

	if mcpData.ToolName != "readFile" {
		t.Errorf("Expected tool name 'readFile', got %s", mcpData.ToolName)
	}

	if mcpData.Content != "File contents here" {
		t.Errorf("Expected content 'File contents here', got %s", mcpData.Content)
	}

	if mcpData.Error != "" {
		t.Errorf("Expected no error, got %s", mcpData.Error)
	}

	// Verify the mock was called correctly
	if mockClient.CallToolCallCount() != 1 {
		t.Errorf("Expected CallTool to be called once, got %d", mockClient.CallToolCallCount())
	}

	_, toolName, actualArgs := mockClient.CallToolArgsForCall(0)
	if toolName != "readFile" {
		t.Errorf("Expected tool name 'readFile', got %s", toolName)
	}

	if actualArgs["path"] != "/test/file.txt" {
		t.Errorf("Expected path '/test/file.txt', got %v", actualArgs["path"])
	}
}

func TestMCPTool_Execute_Error(t *testing.T) {
	mcpConfig := &config.MCPConfig{
		Enabled: true,
		Servers: []config.MCPServerEntry{
			{
				Name:    "test-server",
				Enabled: true,
			},
		},
	}

	mockClient := &mcpmocks.FakeClient{}

	mockClient.CallToolReturns(mcpdomain.CallResult{}, errors.New("connection failed"))

	tool := NewMCPTool(
		"test-server",
		"readFile",
		"Reads a file",
		nil,
		mockClient,
		mcpConfig,
	)

	ctx := context.Background()
	args := map[string]any{"path": "/test/file.txt"}

	result, err := tool.Execute(ctx, args)

	// Execute should return result with error, not an error itself
	if err != nil {
		t.Fatalf("Execute() should not return error, got: %v", err)
	}

	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if result.Success {
		t.Error("Expected failed execution")
	}

	mcpData, ok := result.Data.(*mcpdomain.ToolResult)
	if !ok {
		t.Fatal("Expected result.Data to be *mcpdomain.ToolResult")
	}

	if mcpData.Error == "" {
		t.Error("Expected error message to be set")
	}

	if !strings.Contains(mcpData.Error, "connection failed") {
		t.Errorf("Expected error to contain 'connection failed', got: %s", mcpData.Error)
	}
}

func TestMCPTool_Validate(t *testing.T) {
	tests := []struct {
		name        string
		inputSchema map[string]any
		args        map[string]any
		wantErr     bool
		errorMsg    string
	}{
		{
			name:        "nil args",
			inputSchema: nil,
			args:        nil,
			wantErr:     true,
			errorMsg:    "arguments cannot be nil",
		},
		{
			name: "missing required field",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
			args:     map[string]any{},
			wantErr:  true,
			errorMsg: "required field",
		},
		{
			name: "all required fields present",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []any{"path"},
			},
			args:    map[string]any{"path": "/test/file.txt"},
			wantErr: false,
		},
		{
			name: "invalid field type",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"count": map[string]any{"type": "number"},
				},
			},
			args:     map[string]any{"count": "not a number"},
			wantErr:  true,
			errorMsg: "invalid type",
		},
		{
			name: "valid field types",
			inputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":  map[string]any{"type": "string"},
					"count": map[string]any{"type": "number"},
					"force": map[string]any{"type": "boolean"},
				},
			},
			args: map[string]any{
				"path":  "/test/file.txt",
				"count": float64(5),
				"force": true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpConfig := &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:    "test-server",
						Enabled: true,
					},
				},
			}

			mockClient := &mcpmocks.FakeClient{}

			tool := NewMCPTool(
				"test-server",
				"testTool",
				"Test tool",
				tt.inputSchema,
				mockClient,
				mcpConfig,
			)

			err := tool.Validate(tt.args)

			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errorMsg != "" {
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error to contain %q, got: %v", tt.errorMsg, err)
				}
			}
		})
	}
}

func TestMCPTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name       string
		mcpConfig  *config.MCPConfig
		serverName string
		toolName   string
		expected   bool
	}{
		{
			name: "enabled server and tool",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:    "test-server",
						Enabled: true,
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   true,
		},
		{
			name: "disabled server",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:    "test-server",
						Enabled: false,
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   false,
		},
		{
			name: "MCP globally disabled",
			mcpConfig: &config.MCPConfig{
				Enabled: false,
				Servers: []config.MCPServerEntry{
					{
						Name:    "test-server",
						Enabled: true,
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   false,
		},
		{
			name: "server not found",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:    "other-server",
						Enabled: true,
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   false,
		},
		{
			name: "tool excluded",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:         "test-server",
						Enabled:      true,
						ExcludeTools: []string{"readFile"},
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   false,
		},
		{
			name: "tool in include list",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:         "test-server",
						Enabled:      true,
						IncludeTools: []string{"readFile"},
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   true,
		},
		{
			name: "tool not in include list",
			mcpConfig: &config.MCPConfig{
				Enabled: true,
				Servers: []config.MCPServerEntry{
					{
						Name:         "test-server",
						Enabled:      true,
						IncludeTools: []string{"writeFile"},
					},
				},
			},
			serverName: "test-server",
			toolName:   "readFile",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &mcpmocks.FakeClient{}

			tool := NewMCPTool(
				tt.serverName,
				tt.toolName,
				"Test tool",
				nil,
				mockClient,
				tt.mcpConfig,
			)

			result := tool.IsEnabled()

			if result != tt.expected {
				t.Errorf("IsEnabled() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestMCPTool_ShouldCollapseArg(t *testing.T) {
	mockClient := &mcpmocks.FakeClient{}
	tool := NewMCPTool("server", "tool", "desc", nil, mockClient, &config.MCPConfig{})

	tests := []struct {
		key      string
		expected bool
	}{
		{"content", true},
		{"data", true},
		{"text", true},
		{"path", false},
		{"name", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := tool.ShouldCollapseArg(tt.key)
			if result != tt.expected {
				t.Errorf("ShouldCollapseArg(%s) = %v, expected %v", tt.key, result, tt.expected)
			}
		})
	}
}

func TestMCPTool_ShouldAlwaysExpand(t *testing.T) {
	mockClient := &mcpmocks.FakeClient{}
	tool := NewMCPTool("server", "tool", "desc", nil, mockClient, &config.MCPConfig{})

	if tool.ShouldAlwaysExpand() {
		t.Error("Expected ShouldAlwaysExpand to return false for MCP tools")
	}
}

// A result the tool itself flagged with isError fails the execution even
// though the JSON-RPC call succeeded.
func TestMCPTool_Execute_ToolReportedError(t *testing.T) {
	mockClient := &mcpmocks.FakeClient{}
	mockClient.CallToolReturns(mcpdomain.CallResult{Content: "no such file", IsError: true}, nil)
	tool := NewMCPTool("server", "readFile", "desc", nil, mockClient, &config.MCPConfig{})

	result, err := tool.Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("Execute() should not return error, got: %v", err)
	}
	if result.Success || !strings.Contains(result.Error, "no such file") {
		t.Errorf("expected a failed execution naming the tool's error, got success=%v error=%q", result.Success, result.Error)
	}
}
