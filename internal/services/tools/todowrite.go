package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// TodoWriteTool handles structured task list management for coding sessions
type TodoWriteTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewTodoWriteTool creates a new TodoWrite tool
func NewTodoWriteTool(cfg *config.Config) *TodoWriteTool {
	return &TodoWriteTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.TodoWrite.Enabled,
		formatter: domain.NewBaseFormatter("TodoWrite"),
	}
}

// Definition returns the tool definition for the LLM
func (t *TodoWriteTool) Definition() sdk.ChatCompletionTool {
	description := `Use this tool to create and manage a structured task list for your current coding session. This helps you track progress, organize complex tasks, and demonstrate thoroughness to the user.
It also helps the user understand the progress of the task and overall progress of their requests.

## When to Use This Tool
Use this tool proactively in these scenarios:

1. Complex multi-step tasks - When a task requires 3 or more distinct steps or actions
2. Non-trivial and complex tasks - Tasks that require careful planning or multiple operations
3. User explicitly requests todo list - When the user directly asks you to use the todo list
4. User provides multiple tasks - When users provide a list of things to be done (numbered or comma-separated)
5. After receiving new instructions - Immediately capture user requirements as todos
6. When you start working on a task - Mark it as in_progress BEFORE beginning work. Ideally you should only have one todo as in_progress at a time
7. After completing a task - Mark it as completed and add any new follow-up tasks discovered during implementation

## When NOT to Use This Tool

Skip using this tool when:
1. There is only a single, straightforward task
2. The task is trivial and tracking it provides no organizational benefit
3. The task can be completed in less than 3 trivial steps
4. The task is purely conversational or informational

NOTE that you should not use this tool if there is only one trivial task to do. In this case you are better off just doing the task directly.

## Task States and Management

1. **Task States**: Use these states to track progress:
   - pending: Task not yet started
   - in_progress: Currently working on (limit to ONE task at a time)
   - completed: Task finished successfully

2. **Task Management**:
   - Update task status in real-time as you work
   - Mark tasks complete IMMEDIATELY after finishing (don't batch completions)
   - Only have ONE task in_progress at any time
   - Complete current tasks before starting new ones
   - Remove tasks that are no longer relevant from the list entirely

3. **Task Completion Requirements**:
   - ONLY mark a task as completed when you have FULLY accomplished it
   - If you encounter errors, blockers, or cannot finish, keep the task as in_progress
   - When blocked, create a new task describing what needs to be resolved
   - Never mark a task as completed if:
     - Tests are failing
     - Implementation is partial
     - You encountered unresolved errors
     - You couldn't find necessary files or dependencies

4. **Task Breakdown**:
   - Create specific, actionable items
   - Break complex tasks into smaller, manageable steps
   - Use clear, descriptive task names

When in doubt, use this tool. Being proactive with task management demonstrates attentiveness and ensures you complete all requirements successfully.`
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
							"required":             []string{"content", "status", "id"},
							"properties": map[string]any{
								"content": map[string]any{
									"type":      "string",
									"minLength": 1,
								},
								"id": map[string]any{
									"type": "string",
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
func (t *TodoWriteTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("TodoWrite tool is not enabled")
	}

	todos, ok := args["todos"].([]any)
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "TodoWrite",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "todos parameter is required and must be an array",
		}, nil
	}

	todoResult, err := t.executeTodoWrite(todos)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "TodoWrite",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &domain.ToolExecutionResult{
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
func (t *TodoWriteTool) executeTodoWrite(todosRaw []any) (*domain.TodoWriteToolResult, error) {
	var todos []domain.TodoItem

	for i, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("todo item at index %d must be an object", i)
		}

		todo := domain.TodoItem{}

		if id, ok := todoMap["id"].(string); ok {
			todo.ID = id
		} else {
			return nil, fmt.Errorf("todo item at index %d: id is required and must be a string", i)
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

	result := &domain.TodoWriteToolResult{
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

	var todos []domain.TodoItem
	for i, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]any)
		if !ok {
			return fmt.Errorf("todo item at index %d must be an object", i)
		}

		todo := domain.TodoItem{}

		if id, ok := todoMap["id"].(string); ok {
			todo.ID = id
		} else {
			return fmt.Errorf("todo item at index %d: id is required and must be a string", i)
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
func (t *TodoWriteTool) validateTodoList(todos []domain.TodoItem) error {
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
func (t *TodoWriteTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *TodoWriteTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	todoResult, ok := result.Data.(*domain.TodoWriteToolResult)
	if !ok {
		if result.Success {
			return "Todo list updated successfully"
		}
		return fmt.Sprintf("%s Todo list update failed", icons.CrossMarkStyle.Render(icons.CrossMark))
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
func (t *TodoWriteTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("└─ %s %s", statusIcon, preview))

	return output.String()
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *TodoWriteTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var output strings.Builder

	output.WriteString(t.formatExpandedHeader(result))

	if result.Data != nil {
		dataContent := t.formatTodoData(result.Data)
		hasMetadata := len(result.Metadata) > 0
		output.WriteString(t.formatter.FormatDataSection(dataContent, hasMetadata))
	}

	hasDataSection := result.Data != nil
	output.WriteString(t.formatter.FormatExpandedFooter(result, hasDataSection))

	return output.String()
}

// formatExpandedHeader formats the expanded view header with TodoWrite-specific collapse logic
func (t *TodoWriteTool) formatExpandedHeader(result *domain.ToolExecutionResult) string {
	var output strings.Builder
	toolCall := t.formatToolCallWithCollapse(result.Arguments)

	output.WriteString(fmt.Sprintf("%s\n", toolCall))
	output.WriteString(fmt.Sprintf("├─ ⏱️  Duration: %s\n", t.formatter.FormatDuration(result)))
	output.WriteString(fmt.Sprintf("├─ 📊 Status: %s\n", t.formatter.FormatStatus(result.Success)))

	if result.Error != "" {
		output.WriteString(fmt.Sprintf("├─ %s Error: %s\n", icons.CrossMarkStyle.Render(icons.CrossMark), result.Error))
	}

	if len(result.Arguments) > 0 {
		output.WriteString("├─ 📝 Arguments:\n")
		keys := make([]string, 0, len(result.Arguments))
		for key := range result.Arguments {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for i, key := range keys {
			value := result.Arguments[key]
			if t.ShouldCollapseArg(key) {
				value = "..."
			}
			hasMore := i < len(keys)-1 || result.Data != nil || len(result.Metadata) > 0
			if hasMore {
				output.WriteString(fmt.Sprintf("│  ├─ %s: %v\n", key, value))
			} else {
				output.WriteString(fmt.Sprintf("│  └─ %s: %v\n", key, value))
			}
		}
	}

	return output.String()
}

// formatToolCallWithCollapse formats a tool call with TodoWrite-specific collapse logic
func (t *TodoWriteTool) formatToolCallWithCollapse(args map[string]any) string {
	if len(args) == 0 {
		return "TodoWrite()"
	}

	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	argPairs := make([]string, 0, len(args))
	for _, key := range keys {
		value := args[key]
		if t.ShouldCollapseArg(key) {
			value = `"..."`
		}
		argPairs = append(argPairs, fmt.Sprintf("%s=%v", key, value))
	}

	return fmt.Sprintf("TodoWrite(%s)", strings.Join(argPairs, ", "))
}

// formatTodoData formats todo-specific data with progress visualization
func (t *TodoWriteTool) formatTodoData(data any) string {
	todoResult, ok := data.(*domain.TodoWriteToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}

	var output strings.Builder

	header := colors.CreateColoredText("Todo List", colors.AccentColor)
	completionText := colors.CreateColoredText(fmt.Sprintf("(%d/%d completed)", todoResult.CompletedTasks, todoResult.TotalTasks), colors.DimColor)
	output.WriteString(fmt.Sprintf("%s %s\n\n", header, completionText))

	if todoResult.TotalTasks > 0 {
		progressBar := t.formatColoredProgressBar(todoResult.CompletedTasks, todoResult.TotalTasks)
		percentage := int(float64(todoResult.CompletedTasks) / float64(todoResult.TotalTasks) * 100)
		progressText := colors.CreateColoredText(fmt.Sprintf("Progress: %s %d%%", progressBar, percentage), colors.AccentColor)
		output.WriteString(fmt.Sprintf("%s\n\n", progressText))
	}

	if len(todoResult.Todos) > 0 {
		for _, todo := range todoResult.Todos {
			checkbox, content := t.formatTodoItem(todo)
			output.WriteString(fmt.Sprintf("%s %s\n", checkbox, content))
		}
	}

	if todoResult.InProgressTask != "" {
		workingText := colors.CreateColoredText("🚧 Currently working on:", colors.AccentColor)
		taskText := colors.CreateColoredText(todoResult.InProgressTask, colors.SuccessColor)
		output.WriteString(fmt.Sprintf("\n%s %s\n", workingText, taskText))
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

// formatColoredProgressBar creates a beautiful colored progress bar
func (t *TodoWriteTool) formatColoredProgressBar(completed, total int) string {
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

// formatTodoItem formats a single todo item with appropriate colors and icons
func (t *TodoWriteTool) formatTodoItem(todo domain.TodoItem) (string, string) {
	var checkbox, content string

	switch todo.Status {
	case "completed":
		checkbox = colors.CreateColoredText("☐", colors.SuccessColor)
		content = colors.CreateStrikethroughText(todo.Content)
	case "in_progress":
		checkbox = colors.CreateColoredText("☐", colors.AccentColor)
		content = colors.CreateColoredText(fmt.Sprintf("%s (in progress)", todo.Content), colors.AccentColor)
	default:
		checkbox = "☐"
		content = todo.Content
	}

	return checkbox, content
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TodoWriteTool) ShouldCollapseArg(key string) bool {
	return key == "todos"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TodoWriteTool) ShouldAlwaysExpand() bool {
	return true
}
