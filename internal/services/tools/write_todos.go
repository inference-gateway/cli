package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
)

// WriteTodosTool handles in-memory todo list management for planning tasks
type WriteTodosTool struct {
	config  *config.Config
	enabled bool
}

// NewWriteTodosTool creates a new write todos tool
func NewWriteTodosTool(cfg *config.Config) *WriteTodosTool {
	return &WriteTodosTool{
		config:  cfg,
		enabled: cfg.Tools.Enabled && cfg.Tools.WriteTodos.Enabled,
	}
}

// Definition returns the tool definition for the LLM
func (t *WriteTodosTool) Definition() domain.ToolDefinition {
	return domain.ToolDefinition{
		Name:        "WriteTodos",
		Description: "Write and manage an in-memory todo list for task planning and tracking. This tool helps LLMs organize and track work step-by-step for better transparency.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todos": map[string]interface{}{
					"type":        "array",
					"description": "Array of todo items to store in memory",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"id": map[string]interface{}{
								"type":        "string",
								"description": "Unique identifier for the todo item",
							},
							"content": map[string]interface{}{
								"type":        "string",
								"description": "The todo item content or description",
							},
							"status": map[string]interface{}{
								"type":        "string",
								"description": "Status of the todo item",
								"enum":        []string{"pending", "in_progress", "completed"},
								"default":     "pending",
							},
						},
						"required": []string{"id", "content", "status"},
					},
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format for the response",
					"enum":        []string{"text", "json"},
					"default":     "text",
				},
			},
			"required": []string{"todos"},
		},
	}
}

// Execute runs the write todos tool with given arguments
func (t *WriteTodosTool) Execute(ctx context.Context, args map[string]interface{}) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled {
		return nil, fmt.Errorf("write todos tool is not enabled")
	}

	todosRaw, ok := args["todos"]
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteTodos",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "todos parameter is required",
		}, nil
	}

	todosArray, ok := todosRaw.([]interface{})
	if !ok {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteTodos",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "todos parameter must be an array",
		}, nil
	}

	format := "text"
	if formatRaw, ok := args["format"].(string); ok {
		format = formatRaw
	}

	result, err := t.executeTodosWrite(todosArray, format)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "WriteTodos",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  "WriteTodos",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

// Validate checks if the write todos tool arguments are valid
func (t *WriteTodosTool) Validate(args map[string]interface{}) error {
	if !t.config.Tools.Enabled {
		return fmt.Errorf("write todos tool is not enabled")
	}

	todosRaw, ok := args["todos"]
	if !ok {
		return fmt.Errorf("todos parameter is required")
	}

	todosArray, ok := todosRaw.([]interface{})
	if !ok {
		return fmt.Errorf("todos parameter must be an array")
	}

	if len(todosArray) == 0 {
		return fmt.Errorf("todos array cannot be empty")
	}

	seenIDs := make(map[string]bool)
	for i, todoRaw := range todosArray {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return fmt.Errorf("todo item %d must be an object", i)
		}

		id, hasID := todoMap["id"].(string)
		if !hasID || id == "" {
			return fmt.Errorf("todo item %d must have a non-empty id field", i)
		}

		if seenIDs[id] {
			return fmt.Errorf("duplicate todo id: %s", id)
		}
		seenIDs[id] = true

		content, hasContent := todoMap["content"].(string)
		if !hasContent || content == "" {
			return fmt.Errorf("todo item %d must have a non-empty content field", i)
		}

		status, hasStatus := todoMap["status"].(string)
		if !hasStatus {
			return fmt.Errorf("todo item %d must have a status field", i)
		}

		if status != "pending" && status != "in_progress" && status != "completed" {
			return fmt.Errorf("todo item %d has invalid status: %s (must be 'pending', 'in_progress', or 'completed')", i, status)
		}
	}

	if format, ok := args["format"].(string); ok {
		if format != "text" && format != "json" {
			return fmt.Errorf("format must be 'text' or 'json'")
		}
	} else if args["format"] != nil {
		return fmt.Errorf("format parameter must be a string")
	}

	return nil
}

// IsEnabled returns whether the write todos tool is enabled
func (t *WriteTodosTool) IsEnabled() bool {
	return t.enabled
}

// TodoItem represents a single todo item
type TodoItem struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

// WriteTodosResult represents the result of a write todos operation
type WriteTodosResult struct {
	Todos      []TodoItem `json:"todos"`
	Count      int        `json:"count"`
	Pending    int        `json:"pending"`
	InProgress int        `json:"in_progress"`
	Completed  int        `json:"completed"`
	Format     string     `json:"format"`
	Summary    string     `json:"summary,omitempty"`
}

// executeTodosWrite processes and stores the todo items in memory
func (t *WriteTodosTool) executeTodosWrite(todosArray []interface{}, format string) (*WriteTodosResult, error) {
	var todos []TodoItem
	var pending, inProgress, completed int

	for _, todoRaw := range todosArray {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid todo item format")
		}

		todo := TodoItem{
			ID:      todoMap["id"].(string),
			Content: todoMap["content"].(string),
			Status:  todoMap["status"].(string),
		}

		switch todo.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}

		todos = append(todos, todo)
	}

	result := &WriteTodosResult{
		Todos:      todos,
		Count:      len(todos),
		Pending:    pending,
		InProgress: inProgress,
		Completed:  completed,
		Format:     format,
	}

	if format == "text" {
		result.Summary = t.generateTextSummary(result)
	}

	return result, nil
}

// generateTextSummary creates a human-readable summary of the todos
func (t *WriteTodosTool) generateTextSummary(result *WriteTodosResult) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("Todo List Summary: %d total items", result.Count))
	lines = append(lines, fmt.Sprintf("├─ Pending: %d", result.Pending))
	lines = append(lines, fmt.Sprintf("├─ In Progress: %d", result.InProgress))
	lines = append(lines, fmt.Sprintf("└─ Completed: %d", result.Completed))
	lines = append(lines, "")

	statusOrder := []string{"in_progress", "pending", "completed"}
	statusSymbols := map[string]string{
		"pending":     "◯",
		"in_progress": "◐",
		"completed":   "●",
	}
	statusTitles := map[string]string{
		"pending":     "Pending Tasks",
		"in_progress": "In Progress",
		"completed":   "Completed Tasks",
	}

	for _, status := range statusOrder {
		var statusTodos []TodoItem
		for _, todo := range result.Todos {
			if todo.Status == status {
				statusTodos = append(statusTodos, todo)
			}
		}

		if len(statusTodos) > 0 {
			lines = append(lines, statusTitles[status]+":")
			for _, todo := range statusTodos {
				symbol := statusSymbols[todo.Status]
				lines = append(lines, fmt.Sprintf("  %s %s", symbol, todo.Content))
			}
			lines = append(lines, "")
		}
	}

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}
