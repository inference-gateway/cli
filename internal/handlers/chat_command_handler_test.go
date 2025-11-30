package handlers

import (
	"testing"

	"github.com/inference-gateway/cli/internal/services"
	"github.com/inference-gateway/cli/internal/shortcuts"
	mocks "github.com/inference-gateway/cli/tests/mocks/domain"
	"github.com/stretchr/testify/assert"
)

func TestChatCommandHandler_handleCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		setupMocks  func(*shortcuts.Registry, *mocks.FakeToolService)
		expectError bool
	}{
		{
			name:    "Valid help command",
			command: "/help",
			setupMocks: func(registry *shortcuts.Registry, toolService *mocks.FakeToolService) {
			},
			expectError: false,
		},
		{
			name:    "Valid clear command",
			command: "/clear",
			setupMocks: func(registry *shortcuts.Registry, toolService *mocks.FakeToolService) {
			},
			expectError: false,
		},
		{
			name:    "Invalid command format",
			command: "/invalid-format-command",
			setupMocks: func(registry *shortcuts.Registry, toolService *mocks.FakeToolService) {
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shortcutRegistry := shortcuts.NewRegistry()
			mockTool := &mocks.FakeToolService{}

			if tt.setupMocks != nil {
				tt.setupMocks(shortcutRegistry, mockTool)
			}

			handler := &ChatHandler{
				shortcutRegistry: shortcutRegistry,
				toolService:      mockTool,
			}

			commandHandler := NewChatCommandHandler(handler)

			cmd := commandHandler.handleCommand(tt.command)

			assert.NotNil(t, cmd)
		})
	}
}

func TestChatCommandHandler_handleBashCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		toolEnabled bool
		expectedCmd bool
		expectError bool
	}{
		{
			name:        "Valid bash command with tool enabled",
			command:     "!ls -la",
			toolEnabled: true,
			expectedCmd: true,
			expectError: false,
		},
		{
			name:        "Valid bash command with tool disabled",
			command:     "!ls -la",
			toolEnabled: false,
			expectedCmd: true,
			expectError: true,
		},
		{
			name:        "Empty bash command",
			command:     "!",
			toolEnabled: true,
			expectedCmd: true,
			expectError: true,
		},
		{
			name:        "Complex bash command",
			command:     "!find . -name '*.go' | grep test",
			toolEnabled: true,
			expectedCmd: true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTool := &mocks.FakeToolService{}
			mockTool.IsToolEnabledReturns(tt.toolEnabled)

			mockConfig := &mocks.FakeConfigService{}
			mockConfig.IsBashCommandWhitelistedReturns(true)

			conversationRepo := services.NewInMemoryConversationRepository(nil)

			handler := &ChatHandler{
				toolService:      mockTool,
				configService:    mockConfig,
				conversationRepo: conversationRepo,
			}

			commandHandler := NewChatCommandHandler(handler)

			cmd := commandHandler.handleBashCommand(tt.command)

			if tt.expectedCmd {
				assert.NotNil(t, cmd)
			} else {
				assert.Nil(t, cmd)
			}
		})
	}
}

func TestChatCommandHandler_handleToolCommand(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		toolEnabled bool
		expectError bool
	}{
		{
			name:        "Valid tool command with enabled tool",
			command:     "!!Read(file_path=\"test.txt\")",
			toolEnabled: true,
			expectError: false,
		},
		{
			name:        "Valid tool command with disabled tool",
			command:     "!!Read(file_path=\"test.txt\")",
			toolEnabled: false,
			expectError: true,
		},
		{
			name:        "Invalid tool syntax",
			command:     "!!InvalidSyntax",
			toolEnabled: true,
			expectError: true,
		},
		{
			name:        "Empty tool command",
			command:     "!!",
			toolEnabled: true,
			expectError: true,
		},
		{
			name:        "Tool with multiple arguments",
			command:     "!!Write(file_path=\"test.txt\", content=\"hello world\")",
			toolEnabled: true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTool := &mocks.FakeToolService{}
			mockTool.IsToolEnabledReturns(tt.toolEnabled)

			mockConfig := &mocks.FakeConfigService{}
			mockConfig.IsApprovalRequiredReturns(false)

			conversationRepo := services.NewInMemoryConversationRepository(nil)

			handler := &ChatHandler{
				toolService:      mockTool,
				configService:    mockConfig,
				conversationRepo: conversationRepo,
			}

			commandHandler := NewChatCommandHandler(handler)

			cmd := commandHandler.handleToolCommand(tt.command)

			assert.NotNil(t, cmd)
		})
	}
}

func TestChatCommandHandler_ParseArguments_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    map[string]any
		expectError bool
	}{
		{
			name:        "Boolean values",
			input:       "enabled=true, disabled=false",
			expected:    map[string]any{"enabled": "true", "disabled": "false"},
			expectError: false,
		},
		{
			name:        "Mixed quotes",
			input:       "path='/home/user', mode=\"read\"",
			expected:    map[string]any{"path": "/home/user", "mode": "read"},
			expectError: false,
		},
		{
			name:        "Numeric strings",
			input:       "port=\"8080\", timeout=30",
			expected:    map[string]any{"port": float64(8080), "timeout": float64(30)},
			expectError: false,
		},
		{
			name:        "Special characters in strings",
			input:       "regex=\"[a-zA-Z0-9]+\", path=\"C:\\\\Users\\\\test\"",
			expected:    map[string]any{"regex": "[a-zA-Z0-9]+", "path": "C:\\\\Users\\\\test"},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := &ChatCommandHandler{}

			result, err := handler.ParseArguments(tt.input)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
