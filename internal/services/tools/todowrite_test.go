package tools

import (
	"context"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

func TestTodoWriteTool_Definition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			TodoWrite: config.TodoWriteToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTodoWriteTool(cfg)
	def := tool.Definition()

	if def.Name != "TodoWrite" {
		t.Errorf("Expected tool name 'TodoWrite', got %s", def.Name)
	}

	if def.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if def.Parameters == nil {
		t.Error("Tool parameters should not be nil")
	}
}

func TestTodoWriteTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name          string
		toolsEnabled  bool
		todoEnabled   bool
		expectedState bool
	}{
		{
			name:          "enabled when both tools and todowrite enabled",
			toolsEnabled:  true,
			todoEnabled:   true,
			expectedState: true,
		},
		{
			name:          "disabled when tools disabled",
			toolsEnabled:  false,
			todoEnabled:   true,
			expectedState: false,
		},
		{
			name:          "disabled when todowrite disabled",
			toolsEnabled:  true,
			todoEnabled:   false,
			expectedState: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled: tt.toolsEnabled,
					TodoWrite: config.TodoWriteToolConfig{
						Enabled: tt.todoEnabled,
					},
				},
			}

			tool := NewTodoWriteTool(cfg)
			if tool.IsEnabled() != tt.expectedState {
				t.Errorf("Expected IsEnabled() to return %v, got %v", tt.expectedState, tool.IsEnabled())
			}
		})
	}
}

func TestTodoWriteTool_Validate(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			TodoWrite: config.TodoWriteToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTodoWriteTool(cfg)

	t.Run("valid cases", func(t *testing.T) {
		testValidTodoWriteValidation(t, tool)
	})

	t.Run("invalid cases", func(t *testing.T) {
		testInvalidTodoWriteValidation(t, tool)
	})
}

func testValidTodoWriteValidation(t *testing.T, tool *TodoWriteTool) {
	validTests := []struct {
		name string
		args map[string]interface{}
	}{
		{
			name: "valid todos",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Test task",
						"status":  "pending",
					},
				},
			},
		},
	}

	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err != nil {
				t.Errorf("Expected validation to pass, but got error: %v", err)
			}
		})
	}
}

func testInvalidTodoWriteValidation(t *testing.T, tool *TodoWriteTool) {
	invalidTests := []struct {
		name     string
		args     map[string]interface{}
		errorMsg string
	}{
		{
			name:     "missing todos parameter",
			args:     map[string]interface{}{},
			errorMsg: "todos parameter is required and must be an array",
		},
		{
			name: "todos not array",
			args: map[string]interface{}{
				"todos": "not an array",
			},
			errorMsg: "todos parameter is required and must be an array",
		},
		{
			name: "empty todos array",
			args: map[string]interface{}{
				"todos": []interface{}{},
			},
			errorMsg: "todos array cannot be empty",
		},
		{
			name: "duplicate IDs",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Task 1",
						"status":  "pending",
					},
					map[string]interface{}{
						"id":      "1",
						"content": "Task 2",
						"status":  "pending",
					},
				},
			},
			errorMsg: "duplicate todo ID '1' at index 1",
		},
		{
			name: "multiple in_progress tasks",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Task 1",
						"status":  "in_progress",
					},
					map[string]interface{}{
						"id":      "2",
						"content": "Task 2",
						"status":  "in_progress",
					},
				},
			},
			errorMsg: "only one task can be in_progress at a time, found 2 in_progress tasks",
		},
		{
			name: "invalid status",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Task 1",
						"status":  "invalid_status",
					},
				},
			},
			errorMsg: "todo item at index 0: status must be one of: pending, in_progress, completed",
		},
		{
			name: "empty content",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "",
						"status":  "pending",
					},
				},
			},
			errorMsg: "todo item at index 0: content cannot be empty",
		},
		{
			name: "missing required fields",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id": "1",
					},
				},
			},
		},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if err == nil {
				t.Error("Expected validation to fail, but it passed")
				return
			}

			if tt.errorMsg != "" && err.Error() != tt.errorMsg {
				t.Errorf("Expected error message '%s', got '%s'", tt.errorMsg, err.Error())
			}
		})
	}
}

func TestTodoWriteTool_Execute(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			TodoWrite: config.TodoWriteToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTodoWriteTool(cfg)
	ctx := context.Background()

	tests := []struct {
		name               string
		args               map[string]interface{}
		wantSuccess        bool
		expectedTodos      int
		expectedCompleted  int
		expectedInProgress string
	}{
		{
			name: "successful execution with mixed statuses",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Completed task",
						"status":  "completed",
					},
					map[string]interface{}{
						"id":      "2",
						"content": "In progress task",
						"status":  "in_progress",
					},
					map[string]interface{}{
						"id":      "3",
						"content": "Pending task",
						"status":  "pending",
					},
				},
			},
			wantSuccess:        true,
			expectedTodos:      3,
			expectedCompleted:  1,
			expectedInProgress: "In progress task",
		},
		{
			name: "successful execution with all completed",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Done task 1",
						"status":  "completed",
					},
					map[string]interface{}{
						"id":      "2",
						"content": "Done task 2",
						"status":  "completed",
					},
				},
			},
			wantSuccess:       true,
			expectedTodos:     2,
			expectedCompleted: 2,
		},
		{
			name: "failed execution with invalid data",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "1",
						"content": "Task 1",
						"status":  "in_progress",
					},
					map[string]interface{}{
						"id":      "2",
						"content": "Task 2",
						"status":  "in_progress", // This should fail - multiple in_progress
					},
				},
			},
			wantSuccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(ctx, tt.args)
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}

			if result.Success != tt.wantSuccess {
				t.Errorf("Expected Success=%v, got %v", tt.wantSuccess, result.Success)
			}

			if !tt.wantSuccess {
				return
			}

			if result.Data == nil {
				t.Error("Expected result data to be non-nil")
				return
			}

			todoResult, ok := result.Data.(*domain.TodoWriteToolResult)
			if !ok {
				t.Error("Expected result data to be *domain.TodoWriteToolResult")
				return
			}

			if len(todoResult.Todos) != tt.expectedTodos {
				t.Errorf("Expected %d todos, got %d", tt.expectedTodos, len(todoResult.Todos))
			}

			if todoResult.CompletedTasks != tt.expectedCompleted {
				t.Errorf("Expected %d completed tasks, got %d", tt.expectedCompleted, todoResult.CompletedTasks)
			}

			if todoResult.InProgressTask != tt.expectedInProgress {
				t.Errorf("Expected in progress task '%s', got '%s'", tt.expectedInProgress, todoResult.InProgressTask)
			}

			if !todoResult.ValidationOK {
				t.Error("Expected ValidationOK to be true")
			}
		})
	}
}

func TestTodoWriteTool_Execute_ToolDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			TodoWrite: config.TodoWriteToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewTodoWriteTool(cfg)
	ctx := context.Background()

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "1",
				"content": "Test task",
				"status":  "pending",
			},
		},
	}

	_, err := tool.Execute(ctx, args)
	if err == nil {
		t.Error("Expected Execute to return error when tools are disabled")
	}

	expectedError := "TodoWrite tool is not enabled"
	if err.Error() != expectedError {
		t.Errorf("Expected error '%s', got '%s'", expectedError, err.Error())
	}
}
