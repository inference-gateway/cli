package computer

import (
	"cmp"
	"context"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	computerdomain "github.com/inference-gateway/cli/internal/computer/domain"
)

// recordTool is RecordStart, or RecordStop when stop is set; both drive the
// one shared ScreenRecorder.
type recordTool struct {
	config   *config.Config
	recorder *ScreenRecorder
	stop     bool
}

func (t *recordTool) name() string {
	if t.stop {
		return "RecordStop"
	}
	return "RecordStart"
}

// Definition returns the tool definition for the LLM
func (t *recordTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.RecordStart.Description
	params := sdk.FunctionParameters{
		"type": "object",
		"properties": map[string]any{
			"mode": map[string]any{
				"type":        "string",
				"enum":        []string{"screen", "window", "region"},
				"description": "What to record: the entire primary screen (default), one window, or a region.",
			},
			"window": map[string]any{
				"type":        "string",
				"description": "mode=window only: frontmost (default), app:<name>, pid:<number>, or a bare application name.",
			},
			"region": map[string]any{
				"type":        "object",
				"description": "mode=region only: the rectangle to record, in the frame coordinate space (same space as Computer screenshots).",
				"properties": map[string]any{
					"x":      map[string]any{"type": "integer"},
					"y":      map[string]any{"type": "integer"},
					"width":  map[string]any{"type": "integer"},
					"height": map[string]any{"type": "integer"},
				},
				"required": []string{"x", "y", "width", "height"},
			},
		},
	}
	if t.stop {
		description = t.config.Prompts.Tools.RecordStop.Description
		params = sdk.FunctionParameters{"type": "object", "properties": map[string]any{}}
	}
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        t.name(),
			Description: &description,
			Parameters:  &params,
		},
	}
}

// parseRecordRequest maps RecordStart arguments onto a request, rejecting
// arguments that do not belong to the chosen mode.
func parseRecordRequest(args map[string]any) (recordRequest, error) {
	mode, _ := args["mode"].(string)
	req := recordRequest{Mode: cmp.Or(mode, "screen")}
	req.Window, _ = args["window"].(string)
	if raw, ok := args["region"].(map[string]any); ok {
		num := func(key string) int { f, _ := raw[key].(float64); return int(f) }
		req.Region = &computerdomain.Region{X: num("x"), Y: num("y"), Width: num("width"), Height: num("height")}
	}

	switch req.Mode {
	case "screen", "window", "region":
	default:
		return req, fmt.Errorf("invalid mode %q: use screen, window, or region", req.Mode)
	}
	if req.Window != "" && req.Mode != "window" {
		return req, fmt.Errorf("window is only valid with mode \"window\"")
	}
	if (req.Region != nil) != (req.Mode == "region") {
		return req, fmt.Errorf("region is required with, and only valid with, mode \"region\"")
	}
	return req, nil
}

// Validate checks if the tool arguments are valid
func (t *recordTool) Validate(args map[string]any) error {
	if t.stop {
		return nil
	}
	_, err := parseRecordRequest(args)
	return err
}

// Execute starts or stops the recording.
func (t *recordTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	var status RecordingStatus
	var err error
	if t.stop {
		status, err = t.recorder.Stop()
	} else {
		var req recordRequest
		if req, err = parseRecordRequest(args); err != nil {
			return nil, err
		}
		status, err = t.recorder.Start(ctx, req)
	}

	result := &agentdomain.ToolExecutionResult{
		ToolName:  t.name(),
		Arguments: args,
		Success:   err == nil,
		Duration:  time.Since(start),
	}
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	result.Data = &status
	return result, nil
}

// IsEnabled returns whether the tool is enabled
func (t *recordTool) IsEnabled() bool {
	return t.config.ComputerUse.Recording.Enabled
}

// FormatResult formats the result based on the requested format type
func (t *recordTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	if formatType == agentdomain.FormatterShort {
		return t.FormatPreview(result)
	}
	return t.FormatForLLM(result)
}

// FormatPreview returns a short preview of the result for UI display
func (t *recordTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return t.name() + " failed"
	}
	if status, ok := result.Data.(*RecordingStatus); ok {
		return status.Message
	}
	return t.name() + " done"
}

// FormatForLLM formats the result for LLM consumption
func (t *recordTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		if result == nil {
			return "Error: no result"
		}
		return fmt.Sprintf("Error: %s", result.Error)
	}
	return t.FormatPreview(result)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *recordTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *recordTool) ShouldAlwaysExpand() bool {
	return false
}
