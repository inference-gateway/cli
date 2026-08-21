package computer

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

var actionKinds = map[string]computerdomain.ActionKind{
	"screenshot":   computerdomain.ActionScreenshot,
	"cursor":       computerdomain.ActionCursor,
	"move":         computerdomain.ActionMove,
	"click":        computerdomain.ActionClick,
	"double_click": computerdomain.ActionDoubleClick,
	"triple_click": computerdomain.ActionTripleClick,
	"scroll":       computerdomain.ActionScroll,
	"type":         computerdomain.ActionType,
	"key":          computerdomain.ActionKey,
}

// ComputerTool is the single computer-use tool: it drives the mouse,
// keyboard, and screen through one action-based interface.
type ComputerTool struct {
	config      *config.Config
	executor    *Executor
	rateLimiter rateLimiter
}

// NewComputerTool creates the Computer tool.
func NewComputerTool(cfg *config.Config, limiter rateLimiter) *ComputerTool {
	return &ComputerTool{config: cfg, executor: NewExecutor(cfg), rateLimiter: limiter}
}

// Definition returns the tool definition for the LLM
func (t *ComputerTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Computer.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Computer",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type":        "string",
						"enum":        []string{"screenshot", "cursor", "move", "click", "double_click", "triple_click", "scroll", "type", "key"},
						"description": "What to do: screenshot (capture the screen, optionally a region), cursor (report the cursor position), move / click / double_click / triple_click (pointer, needs x and y), scroll, type (needs text), key (needs combo).",
					},
					"x": map[string]any{"type": "integer", "description": "Pointer target X in the frame coordinate space (same space as screenshots)."},
					"y": map[string]any{"type": "integer", "description": "Pointer target Y in the frame coordinate space."},
					"region": map[string]any{
						"type":        "object",
						"description": "screenshot only: capture just this rectangle of the frame space at native resolution, so small UI becomes readable.",
						"properties": map[string]any{
							"x":      map[string]any{"type": "integer"},
							"y":      map[string]any{"type": "integer"},
							"width":  map[string]any{"type": "integer"},
							"height": map[string]any{"type": "integer"},
						},
						"required": []string{"x", "y", "width", "height"},
					},
					"text":      map[string]any{"type": "string", "description": "type: the text to type."},
					"combo":     map[string]any{"type": "string", "description": "key: a key combination such as \"enter\", \"cmd+a\", \"ctrl+shift+t\"."},
					"button":    map[string]any{"type": "string", "enum": []string{"left", "right", "middle"}, "description": "click actions: mouse button (default left)."},
					"direction": map[string]any{"type": "string", "enum": []string{"vertical", "horizontal"}, "description": "scroll: axis (default vertical)."},
					"amount":    map[string]any{"type": "integer", "description": "scroll: wheel clicks (default 3); negative scrolls up / left."},
				},
				"required": []string{"action"},
			},
		},
	}
}

// parseAction maps tool arguments onto a domain Action.
func parseAction(args map[string]any) (computerdomain.Action, error) {
	name, _ := args["action"].(string)
	kind, ok := actionKinds[name]
	if !ok {
		return computerdomain.Action{}, fmt.Errorf("invalid action %q", name)
	}

	a := computerdomain.Action{Kind: kind}
	a.Text, _ = args["text"].(string)
	a.Combo, _ = args["combo"].(string)
	a.Button, _ = args["button"].(string)
	a.Direction, _ = args["direction"].(string)
	if f, ok := args["amount"].(float64); ok {
		a.Amount = int(f)
	}

	x, hasX := args["x"].(float64)
	y, hasY := args["y"].(float64)
	if hasX && hasY {
		a.Target = &computerdomain.Target{X: int(x), Y: int(y)}
	}
	if raw, ok := args["region"].(map[string]any); ok {
		num := func(key string) int { f, _ := raw[key].(float64); return int(f) }
		if a.Target == nil {
			a.Target = &computerdomain.Target{}
		}
		a.Target.Region = &computerdomain.Region{X: num("x"), Y: num("y"), Width: num("width"), Height: num("height")}
	}

	switch kind {
	case computerdomain.ActionMove, computerdomain.ActionClick, computerdomain.ActionDoubleClick, computerdomain.ActionTripleClick:
		if !hasX || !hasY {
			return a, fmt.Errorf("action %q requires x and y", name)
		}
	case computerdomain.ActionType:
		if a.Text == "" {
			return a, fmt.Errorf("action \"type\" requires text")
		}
	case computerdomain.ActionKey:
		if a.Combo == "" {
			return a, fmt.Errorf("action \"key\" requires combo")
		}
	}
	return a, nil
}

// Validate checks if the tool arguments are valid
func (t *ComputerTool) Validate(args map[string]any) error {
	_, err := parseAction(args)
	return err
}

// Execute performs the action and returns the observation.
func (t *ComputerTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	a, err := parseAction(args)
	if err != nil {
		return nil, err
	}
	if t.rateLimiter != nil {
		if err := t.rateLimiter.CheckAndRecord("Computer"); err != nil {
			return nil, err
		}
	}

	obs, err := t.executor.Do(ctx, a)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "Computer",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName:  "Computer",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      obs,
	}
	if obs.Image != nil {
		result.Images = []agentdomain.ImageAttachment{{
			Data:        obs.Image.Data,
			MimeType:    obs.Image.MimeType,
			DisplayName: "computer-screenshot",
			SourcePath:  obs.Image.Path,
		}}
	}
	return result, nil
}

// IsEnabled returns whether the tool is enabled
func (t *ComputerTool) IsEnabled() bool {
	return t.config.ComputerUse.Enabled
}

// FormatResult formats the result based on the requested format type
func (t *ComputerTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	if formatType == agentdomain.FormatterShort {
		return t.FormatPreview(result)
	}
	return t.FormatForLLM(result)
}

// FormatPreview returns a short preview of the result for UI display
func (t *ComputerTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Computer action failed"
	}
	if obs, ok := result.Data.(*computerdomain.Observation); ok && obs.Message != "" {
		return obs.Message
	}
	return "Computer action done"
}

// FormatForLLM formats the result for LLM consumption
func (t *ComputerTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	obs, ok := result.Data.(*computerdomain.Observation)
	if !ok {
		return "Computer action done"
	}
	msg := obs.Message
	if obs.Image != nil {
		msg += ". Image is attached"
		if obs.Image.Path != "" {
			msg += fmt.Sprintf(" and saved at %s (use ImageDecode to inspect it if you cannot see images)", obs.Image.Path)
		}
	}
	return msg
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *ComputerTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *ComputerTool) ShouldAlwaysExpand() bool {
	return false
}
