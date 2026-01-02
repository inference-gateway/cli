package tools

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// KeyboardTypeTool types text or sends key combinations
type KeyboardTypeTool struct {
	config      *config.Config
	enabled     bool
	formatter   domain.BaseFormatter
	rateLimiter *RateLimiter
}

// NewKeyboardTypeTool creates a new keyboard type tool
func NewKeyboardTypeTool(cfg *config.Config, rateLimiter *RateLimiter) *KeyboardTypeTool {
	return &KeyboardTypeTool{
		config:      cfg,
		enabled:     cfg.ComputerUse.Enabled && cfg.ComputerUse.KeyboardType.Enabled,
		formatter:   domain.NewBaseFormatter("KeyboardType"),
		rateLimiter: rateLimiter,
	}
}

// Definition returns the tool definition for the LLM
func (t *KeyboardTypeTool) Definition() sdk.ChatCompletionTool {
	description := "Types text or sends key combinations. Can type regular text or send special key combinations like 'ctrl+c'. Requires user approval unless in auto-accept mode. Note: Exactly one of 'text' or 'key_combo' must be provided."
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "KeyboardType",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "Text to type. Mutually exclusive with key_combo.",
					},
					"key_combo": map[string]any{
						"type":        "string",
						"description": "Key combination to send (e.g., 'ctrl+c', 'alt+tab', 'shift+enter'). Mutually exclusive with text.",
					},
					"display": map[string]any{
						"type":        "string",
						"description": "Display to use (e.g., ':0'). Defaults to ':0'.",
						"default":     ":0",
					},
				},
			},
		},
	}
}

// Execute runs the keyboard type tool with given arguments
func (t *KeyboardTypeTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	if err := t.rateLimiter.CheckAndRecord("KeyboardType"); err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	text, hasText := args["text"].(string)
	keyCombo, hasKeyCombo := args["key_combo"].(string)

	if !hasText && !hasKeyCombo {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "either 'text' or 'key_combo' must be provided",
		}, nil
	}

	if hasText && hasKeyCombo {
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "only one of 'text' or 'key_combo' can be provided",
		}, nil
	}

	display := t.config.ComputerUse.Display
	if displayArg, ok := args["display"].(string); ok && displayArg != "" {
		display = displayArg
	}

	displayServer := DetectDisplayServer()

	switch displayServer {
	case DisplayServerX11:
		client, err := NewX11Client(display)
		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "KeyboardType",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}
		defer client.Close()

		if hasText {
			err = client.TypeText(text)
		} else {
			err = client.SendKeyCombo(keyCombo)
		}

		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "KeyboardType",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}

	case DisplayServerWayland:
		client, err := NewWaylandClient(display)
		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "KeyboardType",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}
		defer client.Close()

		if hasText {
			err = client.TypeText(text)
		} else {
			err = client.SendKeyCombo(keyCombo)
		}

		if err != nil {
			return &domain.ToolExecutionResult{
				ToolName:  "KeyboardType",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}

	default:
		return &domain.ToolExecutionResult{
			ToolName:  "KeyboardType",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     "no display server detected (neither X11 nor Wayland)",
		}, nil
	}

	result := domain.KeyboardTypeToolResult{
		Text:     text,
		KeyCombo: keyCombo,
		Display:  display,
		Method:   displayServer.String(),
	}

	return &domain.ToolExecutionResult{
		ToolName:  "KeyboardType",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
	}, nil
}

// Validate checks if the tool arguments are valid
func (t *KeyboardTypeTool) Validate(args map[string]any) error {
	text, hasText := args["text"].(string)
	keyCombo, hasKeyCombo := args["key_combo"].(string)

	if !hasText && !hasKeyCombo {
		return fmt.Errorf("either 'text' or 'key_combo' must be provided")
	}

	if hasText && hasKeyCombo {
		return fmt.Errorf("only one of 'text' or 'key_combo' can be provided")
	}

	if hasText {
		if len(text) > t.config.ComputerUse.KeyboardType.MaxTextLength {
			return fmt.Errorf("text length exceeds maximum of %d characters", t.config.ComputerUse.KeyboardType.MaxTextLength)
		}
		if len(text) == 0 {
			return fmt.Errorf("text cannot be empty")
		}
	}

	if hasKeyCombo {
		if len(keyCombo) == 0 {
			return fmt.Errorf("key_combo cannot be empty")
		}
	}

	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *KeyboardTypeTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *KeyboardTypeTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *KeyboardTypeTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Keyboard input failed"
	}
	data, ok := result.Data.(domain.KeyboardTypeToolResult)
	if !ok {
		return "Keyboard input sent"
	}
	if data.Text != "" {
		preview := data.Text
		if len(preview) > 30 {
			preview = preview[:27] + "..."
		}
		return fmt.Sprintf("Typed: %s", preview)
	}
	return fmt.Sprintf("Key combo: %s", data.KeyCombo)
}

// FormatForLLM formats the result for LLM consumption
func (t *KeyboardTypeTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.KeyboardTypeToolResult)
	if !ok {
		return "Keyboard input sent successfully"
	}
	if data.Text != "" {
		return fmt.Sprintf("Typed text: '%s' using %s", data.Text, data.Method)
	}
	return fmt.Sprintf("Sent key combination '%s' using %s", data.KeyCombo, data.Method)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *KeyboardTypeTool) ShouldCollapseArg(key string) bool {
	return key == "text"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *KeyboardTypeTool) ShouldAlwaysExpand() bool {
	return false
}
