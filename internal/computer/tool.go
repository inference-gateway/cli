package computer

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

var actionKinds = map[string]computerdomain.ActionKind{
	"screenshot":    computerdomain.ActionScreenshot,
	"accessibility": computerdomain.ActionAccessibility,
	"cursor":        computerdomain.ActionCursor,
	"move":          computerdomain.ActionMove,
	"click":         computerdomain.ActionClick,
	"double_click":  computerdomain.ActionDoubleClick,
	"triple_click":  computerdomain.ActionTripleClick,
	"scroll":        computerdomain.ActionScroll,
	"type":          computerdomain.ActionType,
	"key":           computerdomain.ActionKey,
	"press":         computerdomain.ActionPress,
}

// ComputerTool is the single computer-use tool: it drives the mouse,
// keyboard, and screen through one action-based interface.
type ComputerTool struct {
	config      *config.Config
	executor    *Executor
	rateLimiter rateLimiter
}

// NewComputerTool creates the Computer tool. The notifier receives the
// computer-use activity each pointer or keyboard action publishes before it runs.
func NewComputerTool(cfg *config.Config, limiter rateLimiter, notifier agentdomain.UINotifier) *ComputerTool {
	return &ComputerTool{config: cfg, executor: NewExecutor(cfg, notifier), rateLimiter: limiter}
}

// Manifest returns the tool's manifest so the tool registry knows its policy.
func (t *ComputerTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolComputer)
}

// Definition returns the tool definition for the LLM
func (t *ComputerTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
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
	a.Label, _ = args["label"].(string)
	a.Scope, _ = args["target"].(string)
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
	case computerdomain.ActionPress:
		if a.Label == "" {
			return a, fmt.Errorf("action \"press\" requires label")
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
		if err := t.rateLimiter.CheckAndRecord(ToolComputer); err != nil {
			return nil, err
		}
	}

	obs, err := t.executor.Do(ctx, a)
	if err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolComputer,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName:  ToolComputer,
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
	if len(obs.Elements) > 0 {
		if elements, err := json.Marshal(obs.Elements); err == nil {
			msg += "\n" + string(elements)
		}
	}
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
