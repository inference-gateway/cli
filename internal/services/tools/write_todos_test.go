package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/inference-gateway/cli/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWriteTodosTool(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)
	assert.NotNil(t, tool)
	assert.True(t, tool.IsEnabled())
	assert.Equal(t, cfg, tool.config)
}

func TestNewWriteTodosToolDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)
	assert.NotNil(t, tool)
	assert.False(t, tool.IsEnabled())
}

func TestWriteTodosToolDefinition(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)
	def := tool.Definition()

	assert.Equal(t, "WriteTodos", def.Name)
	assert.Contains(t, def.Description, "todo list")
	assert.Contains(t, def.Description, "planning")

	// Verify parameters structure
	params, ok := def.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	// Verify todos parameter
	todos, ok := properties["todos"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "array", todos["type"])

	// Verify format parameter
	format, ok := properties["format"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", format["type"])

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "todos")
}

func TestWriteTodosToolValidateSuccess(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Complete feature implementation",
				"status":  "pending",
			},
			map[string]interface{}{
				"id":      "task-2",
				"content": "Write unit tests",
				"status":  "in_progress",
			},
		},
		"format": "text",
	}

	err := tool.Validate(args)
	assert.NoError(t, err)
}

func getBasicValidationTestCases() []struct {
	name        string
	args        map[string]interface{}
	expectedErr string
} {
	return []struct {
		name        string
		args        map[string]interface{}
		expectedErr string
	}{
		{
			name:        "missing todos parameter",
			args:        map[string]interface{}{},
			expectedErr: "todos parameter is required",
		},
		{
			name: "todos not array",
			args: map[string]interface{}{
				"todos": "not an array",
			},
			expectedErr: "todos parameter must be an array",
		},
		{
			name: "empty todos array",
			args: map[string]interface{}{
				"todos": []interface{}{},
			},
			expectedErr: "todos array cannot be empty",
		},
		{
			name: "todo item not object",
			args: map[string]interface{}{
				"todos": []interface{}{"not an object"},
			},
			expectedErr: "todo item 0 must be an object",
		},
	}
}

func getFieldValidationTestCases() []struct {
	name        string
	args        map[string]interface{}
	expectedErr string
} {
	return []struct {
		name        string
		args        map[string]interface{}
		expectedErr string
	}{
		{
			name: "missing id field",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"content": "test content",
						"status":  "pending",
					},
				},
			},
			expectedErr: "todo item 0 must have a non-empty id field",
		},
		{
			name: "empty id field",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "",
						"content": "test content",
						"status":  "pending",
					},
				},
			},
			expectedErr: "todo item 0 must have a non-empty id field",
		},
		{
			name: "duplicate id",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "first task",
						"status":  "pending",
					},
					map[string]interface{}{
						"id":      "task-1",
						"content": "duplicate task",
						"status":  "pending",
					},
				},
			},
			expectedErr: "duplicate todo id: task-1",
		},
		{
			name: "missing content field",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":     "task-1",
						"status": "pending",
					},
				},
			},
			expectedErr: "todo item 0 must have a non-empty content field",
		},
		{
			name: "empty content field",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "",
						"status":  "pending",
					},
				},
			},
			expectedErr: "todo item 0 must have a non-empty content field",
		},
		{
			name: "missing status field",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "test content",
					},
				},
			},
			expectedErr: "todo item 0 must have a status field",
		},
		{
			name: "invalid status",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "test content",
						"status":  "invalid_status",
					},
				},
			},
			expectedErr: "todo item 0 has invalid status: invalid_status",
		},
	}
}

func getFormatValidationTestCases() []struct {
	name        string
	args        map[string]interface{}
	expectedErr string
} {
	return []struct {
		name        string
		args        map[string]interface{}
		expectedErr string
	}{
		{
			name: "invalid format",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "test content",
						"status":  "pending",
					},
				},
				"format": "xml",
			},
			expectedErr: "format must be 'text' or 'json'",
		},
		{
			name: "format not string",
			args: map[string]interface{}{
				"todos": []interface{}{
					map[string]interface{}{
						"id":      "task-1",
						"content": "test content",
						"status":  "pending",
					},
				},
				"format": 123,
			},
			expectedErr: "format parameter must be a string",
		},
	}
}

func TestWriteTodosToolValidateErrors(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	allTestCases := append(getBasicValidationTestCases(), getFieldValidationTestCases()...)
	allTestCases = append(allTestCases, getFormatValidationTestCases()...)

	for _, tt := range allTestCases {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

func TestWriteTodosToolValidateDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "test",
				"status":  "pending",
			},
		},
	}

	err := tool.Validate(args)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "write todos tool is not enabled")
}

func TestWriteTodosToolExecuteSuccess(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Complete feature implementation",
				"status":  "pending",
			},
			map[string]interface{}{
				"id":      "task-2",
				"content": "Write unit tests",
				"status":  "in_progress",
			},
			map[string]interface{}{
				"id":      "task-3",
				"content": "Update documentation",
				"status":  "completed",
			},
		},
		"format": "json",
	}

	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, "WriteTodos", result.ToolName)
	assert.Greater(t, result.Duration.Nanoseconds(), int64(0))

	// Verify result data
	resultData, ok := result.Data.(*WriteTodosResult)
	require.True(t, ok)
	assert.Equal(t, 3, resultData.Count)
	assert.Equal(t, 1, resultData.Pending)
	assert.Equal(t, 1, resultData.InProgress)
	assert.Equal(t, 1, resultData.Completed)
	assert.Equal(t, "json", resultData.Format)
	assert.Len(t, resultData.Todos, 3)

	// Verify individual todos
	todos := resultData.Todos
	assert.Equal(t, "task-1", todos[0].ID)
	assert.Equal(t, "Complete feature implementation", todos[0].Content)
	assert.Equal(t, "pending", todos[0].Status)

	assert.Equal(t, "task-2", todos[1].ID)
	assert.Equal(t, "Write unit tests", todos[1].Content)
	assert.Equal(t, "in_progress", todos[1].Status)

	assert.Equal(t, "task-3", todos[2].ID)
	assert.Equal(t, "Update documentation", todos[2].Content)
	assert.Equal(t, "completed", todos[2].Status)
}

func TestWriteTodosToolExecuteTextFormat(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Complete feature implementation",
				"status":  "pending",
			},
			map[string]interface{}{
				"id":      "task-2",
				"content": "Write unit tests",
				"status":  "in_progress",
			},
		},
		"format": "text",
	}

	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, result.Success)

	resultData, ok := result.Data.(*WriteTodosResult)
	require.True(t, ok)
	assert.Equal(t, "text", resultData.Format)
	assert.NotEmpty(t, resultData.Summary)
	assert.Contains(t, resultData.Summary, "Todo List Summary: 2 total items")
	assert.Contains(t, resultData.Summary, "Pending: 1")
	assert.Contains(t, resultData.Summary, "In Progress: 1")
	assert.Contains(t, resultData.Summary, "◐ Write unit tests")
	assert.Contains(t, resultData.Summary, "◯ Complete feature implementation")
}

func TestWriteTodosToolExecuteDefaultFormat(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "Test task",
				"status":  "pending",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, result.Success)

	resultData, ok := result.Data.(*WriteTodosResult)
	require.True(t, ok)
	assert.Equal(t, "text", resultData.Format) // defaults to text
	assert.NotEmpty(t, resultData.Summary)
}

func TestWriteTodosToolExecuteErrors(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	tests := []struct {
		name        string
		args        map[string]interface{}
		expectedErr string
	}{
		{
			name:        "missing todos parameter",
			args:        map[string]interface{}{},
			expectedErr: "todos parameter is required",
		},
		{
			name: "todos not array",
			args: map[string]interface{}{
				"todos": "not an array",
			},
			expectedErr: "todos parameter must be an array",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tt.args)
			require.NoError(t, err)
			assert.False(t, result.Success)
			assert.Contains(t, result.Error, tt.expectedErr)
		})
	}
}

func TestWriteTodosToolExecuteDisabled(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: false,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	args := map[string]interface{}{
		"todos": []interface{}{
			map[string]interface{}{
				"id":      "task-1",
				"content": "test",
				"status":  "pending",
			},
		},
	}

	result, err := tool.Execute(context.Background(), args)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "write todos tool is not enabled")
}

func TestGenerateTextSummary(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			WriteTodos: config.WriteTodosToolConfig{
				Enabled: true,
			},
		},
	}

	tool := NewWriteTodosTool(cfg)

	result := &WriteTodosResult{
		Todos: []TodoItem{
			{ID: "1", Content: "Task in progress", Status: "in_progress"},
			{ID: "2", Content: "Pending task", Status: "pending"},
			{ID: "3", Content: "Completed task", Status: "completed"},
		},
		Count:      3,
		Pending:    1,
		InProgress: 1,
		Completed:  1,
		Format:     "text",
	}

	summary := tool.generateTextSummary(result)

	// Verify summary structure
	assert.Contains(t, summary, "Todo List Summary: 3 total items")
	assert.Contains(t, summary, "├─ Pending: 1")
	assert.Contains(t, summary, "├─ In Progress: 1")
	assert.Contains(t, summary, "└─ Completed: 1")

	// Verify tasks are grouped by status
	assert.Contains(t, summary, "In Progress:")
	assert.Contains(t, summary, "◐ Task in progress")
	assert.Contains(t, summary, "Pending Tasks:")
	assert.Contains(t, summary, "◯ Pending task")
	assert.Contains(t, summary, "Completed Tasks:")
	assert.Contains(t, summary, "● Completed task")
}

func TestWriteTodosResultJSONSerialization(t *testing.T) {
	result := &WriteTodosResult{
		Todos: []TodoItem{
			{ID: "task-1", Content: "Test task", Status: "pending"},
		},
		Count:      1,
		Pending:    1,
		InProgress: 0,
		Completed:  0,
		Format:     "json",
	}

	jsonData, err := json.Marshal(result)
	require.NoError(t, err)

	var unmarshaled WriteTodosResult
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, result.Count, unmarshaled.Count)
	assert.Equal(t, result.Pending, unmarshaled.Pending)
	assert.Equal(t, result.Format, unmarshaled.Format)
	assert.Len(t, unmarshaled.Todos, 1)
	assert.Equal(t, result.Todos[0].ID, unmarshaled.Todos[0].ID)
}
