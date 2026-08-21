package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
)

// TodoWriteTool handles structured task list management for coding sessions
type TodoWriteTool struct {
	config    *config.Config
	enabled   bool
	formatter agentinfra.BaseFormatter
}

// NewTodoWriteTool creates a new TodoWrite tool
func NewTodoWriteTool(cfg *config.Config) *TodoWriteTool {
	return &TodoWriteTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.TodoWrite.Enabled,
		formatter: agentinfra.NewBaseFormatter("TodoWrite"),
	}
}

// Definition returns the tool definition for the LLM
func (t *TodoWriteTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.TodoWrite.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "TodoWrite",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"additionalProperties": false,
				"type":                 "object",
				"required":             []string{"todos"},
				"properties": map[string]any{
					"todos": map[string]any{
						"description": "The updated todo list",
						"type":        "array",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"content", "status"},
							"properties": map[string]any{
								"content": map[string]any{
									"type":      "string",
									"minLength": 1,
								},
								"id": map[string]any{
									"type":        "string",
									"description": "Optional unique identifier. If not provided, will be auto-generated.",
								},
								"status": map[string]any{
									"type": "string",
									"enum": []string{"pending", "in_progress", "completed"},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Execute runs the TodoWrite tool with given arguments
func (t *TodoWriteTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("TodoWrite tool is not enabled")
	}

	todos, ok := args["todos"].([]any)
	if !ok {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "TodoWrite",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "todos parameter is required and must be an array",
		}, nil
	}

	todoResult, err := t.executeTodoWrite(todos)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "TodoWrite",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName:  "TodoWrite",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      todoResult,
	}

	return result, nil
}

// Validate checks if the TodoWrite tool arguments are valid
func (t *TodoWriteTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("TodoWrite tool is not enabled")
	}

	todos, ok := args["todos"].([]any)
	if !ok {
		return fmt.Errorf("todos parameter is required and must be an array")
	}

	return t.validateTodos(todos)
}

// IsEnabled returns whether the TodoWrite tool is enabled
func (t *TodoWriteTool) IsEnabled() bool {
	return t.enabled
}

// executeTodoWrite processes the todo list update
func (t *TodoWriteTool) executeTodoWrite(todosRaw []any) (*agentdomain.TodoWriteToolResult, error) {
	var todos []agentdomain.TodoItem

	for i, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("todo item at index %d must be an object", i)
		}

		todo := agentdomain.TodoItem{}

		if id, ok := todoMap["id"].(string); ok && id != "" {
			todo.ID = id
		} else {
			todo.ID = fmt.Sprintf("todo-%d-%d", time.Now().UnixNano(), i)
		}

		if content, ok := todoMap["content"].(string); ok {
			todo.Content = content
		} else {
			return nil, fmt.Errorf("todo item at index %d: content is required and must be a string", i)
		}

		if status, ok := todoMap["status"].(string); ok {
			todo.Status = status
		} else {
			return nil, fmt.Errorf("todo item at index %d: status is required and must be a string", i)
		}

		todos = append(todos, todo)
	}

	if err := t.validateTodoList(todos); err != nil {
		return nil, err
	}

	completedCount := 0
	inProgressTask := ""
	for _, todo := range todos {
		switch todo.Status {
		case "completed":
			completedCount++
		case "in_progress":
			inProgressTask = todo.Content
		}
	}

	result := &agentdomain.TodoWriteToolResult{
		Todos:          todos,
		TotalTasks:     len(todos),
		CompletedTasks: completedCount,
		InProgressTask: inProgressTask,
		ValidationOK:   true,
	}

	return result, nil
}

// validateTodos validates the raw todos array
func (t *TodoWriteTool) validateTodos(todosRaw []any) error {
	if len(todosRaw) == 0 {
		return fmt.Errorf("todos array cannot be empty")
	}

	var todos []agentdomain.TodoItem
	for i, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("todo item at index %d must be an object", i)
		}

		todo := agentdomain.TodoItem{}

		if id, ok := todoMap["id"].(string); ok && id != "" {
			todo.ID = id
		} else {
			todo.ID = fmt.Sprintf("todo-%d-%d", time.Now().UnixNano(), i)
		}

		if content, ok := todoMap["content"].(string); ok {
			todo.Content = content
		} else {
			return fmt.Errorf("todo item at index %d: content is required and must be a string", i)
		}

		if status, ok := todoMap["status"].(string); ok {
			todo.Status = status
		} else {
			return fmt.Errorf("todo item at index %d: status is required and must be a string", i)
		}

		todos = append(todos, todo)
	}

	return t.validateTodoList(todos)
}

// validateTodoList validates business rules for the todo list
func (t *TodoWriteTool) validateTodoList(todos []agentdomain.TodoItem) error {
	idMap := make(map[string]bool)
	inProgressCount := 0

	for i, todo := range todos {
		if idMap[todo.ID] {
			return fmt.Errorf("duplicate todo ID '%s' at index %d", todo.ID, i)
		}
		idMap[todo.ID] = true

		if todo.Content == "" {
			return fmt.Errorf("todo item at index %d: content cannot be empty", i)
		}

		if todo.Status != "pending" && todo.Status != "in_progress" && todo.Status != "completed" {
			return fmt.Errorf("todo item at index %d: status must be one of: pending, in_progress, completed", i)
		}

		if todo.Status == "in_progress" {
			inProgressCount++
		}
	}

	if inProgressCount > 1 {
		return fmt.Errorf("only one task can be in_progress at a time, found %d in_progress tasks", inProgressCount)
	}

	return nil
}

// FormatResult formats tool execution results for different contexts
func (t *TodoWriteTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *TodoWriteTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	todoResult, ok := result.Data.(*agentdomain.TodoWriteToolResult)
	if !ok {
		if result.Success {
			return "Todo list updated successfully"
		}
		return "✗ Todo list update failed"
	}

	if todoResult.TotalTasks == 0 {
		return "Todo list is empty"
	}

	progressBar := t.formatProgressBar(todoResult.CompletedTasks, todoResult.TotalTasks)
	percentage := int(float64(todoResult.CompletedTasks) / float64(todoResult.TotalTasks) * 100)

	status := fmt.Sprintf("%s %d/%d tasks (%d%%)", progressBar, todoResult.CompletedTasks, todoResult.TotalTasks, percentage)

	if todoResult.InProgressTask != "" {
		taskPreview := t.formatter.TruncateText(todoResult.InProgressTask, 30)
		status += fmt.Sprintf(" %s", taskPreview)
	}

	return status
}

// FormatForUI formats the result for UI display
// Returns minimal format as todo updates are shown in the dedicated todo component
func (t *TodoWriteTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	return fmt.Sprintf("TodoWrite(...)\n└─ %s Todo list updated", statusIcon)
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *TodoWriteTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent = t.formatTodoData(result.Data)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatTodoData formats todo-specific data with progress visualization
func (t *TodoWriteTool) formatTodoData(data any) string {
	todoResult, ok := data.(*agentdomain.TodoWriteToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder

	fmt.Fprintf(&output, "Todo List (%d/%d completed)\n\n", todoResult.CompletedTasks, todoResult.TotalTasks)

	if todoResult.TotalTasks > 0 {
		progressBar := t.formatExpandedProgressBar(todoResult.CompletedTasks, todoResult.TotalTasks)
		percentage := int(float64(todoResult.CompletedTasks) / float64(todoResult.TotalTasks) * 100)
		fmt.Fprintf(&output, "Progress: %s %d%%\n\n", progressBar, percentage)
	}

	if len(todoResult.Todos) > 0 {
		for _, todo := range todoResult.Todos {
			checkbox, content := t.formatTodoItem(todo)
			fmt.Fprintf(&output, "%s %s\n", checkbox, content)
		}
	}

	if todoResult.InProgressTask != "" {
		fmt.Fprintf(&output, "\nCurrently working on: %s\n", todoResult.InProgressTask)
	}

	return output.String()
}

// formatProgressBar creates a visual progress bar (simple version for preview)
func (t *TodoWriteTool) formatProgressBar(completed, total int) string {
	if total == 0 {
		return "[----------] 0%"
	}

	barLength := 10
	progress := int(float64(completed) / float64(total) * float64(barLength))

	var bar strings.Builder
	bar.WriteString("[")
	for i := 0; i < barLength; i++ {
		if i < progress {
			bar.WriteString("█")
		} else {
			bar.WriteString("-")
		}
	}
	bar.WriteString("]")

	return bar.String()
}

// formatExpandedProgressBar creates the wide progress bar for the expanded view
func (t *TodoWriteTool) formatExpandedProgressBar(completed, total int) string {
	if total == 0 {
		return "[░░░░░░░░░░]"
	}

	barLength := 20
	progress := int(float64(completed) / float64(total) * float64(barLength))

	var bar strings.Builder
	bar.WriteString("[")
	for i := 0; i < barLength; i++ {
		if i < progress {
			bar.WriteString("█")
		} else {
			bar.WriteString("░")
		}
	}
	bar.WriteString("]")

	return bar.String()
}

// formatTodoItem formats a single todo item as plain text.
func (t *TodoWriteTool) formatTodoItem(todo agentdomain.TodoItem) (string, string) {
	switch todo.Status {
	case "completed":
		return "☒", todo.Content
	case "in_progress":
		return "☐", fmt.Sprintf("%s (in progress)", todo.Content)
	default:
		return "☐", todo.Content
	}
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TodoWriteTool) ShouldCollapseArg(key string) bool {
	return key == "todos"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
// Returns false since todos are shown in the dedicated todo component
func (t *TodoWriteTool) ShouldAlwaysExpand() bool {
	return false
}
