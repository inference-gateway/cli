package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	audio "github.com/inference-gateway/cli/internal/audio"
)

// TextToSFXTool generates a short sound effect or ambience clip from a text
// prompt through the gateway's SFX API and saves it as a WAV file.
type TextToSFXTool struct {
	config *config.Config
	sfx    agentdomain.SoundEffectService
}

// NewTextToSFXTool creates a new TextToSFX tool.
func NewTextToSFXTool(cfg *config.Config, sfx agentdomain.SoundEffectService) *TextToSFXTool {
	return &TextToSFXTool{
		config: cfg,
		sfx:    sfx,
	}
}

// Definition returns the tool definition for TextToSFX
func (t *TextToSFXTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.TextToSFX.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "TextToSFX",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Description of the sound to generate - the event or atmosphere and its character (e.g. a whoosh, a click, a riser, room tone)",
					},
					"seconds": map[string]any{
						"type":        "number",
						"description": "Optional clip length in seconds (0.5-30); omitted lets the provider pick a length that fits the prompt",
					},
					"loop": map[string]any{
						"type":        "boolean",
						"description": "Optional: true to generate a clip that loops seamlessly",
					},
					"output_path": map[string]any{
						"type":        "string",
						"description": "Optional bare file name (no directories or absolute paths) for the generated WAV; it is always placed in the configured output directory. Defaults to a timestamped file",
					},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates TextToSFX arguments
func (t *TextToSFXTool) Validate(args map[string]any) error {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required and must be a non-empty string")
	}

	if raw, present := args["seconds"]; present && raw != nil {
		seconds, ok := raw.(float64)
		if !ok {
			return fmt.Errorf("seconds must be a number")
		}
		if seconds < 0.5 || seconds > 30 {
			return fmt.Errorf("seconds must be between 0.5 and 30")
		}
	}

	if raw, present := args["loop"]; present && raw != nil {
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("loop must be a boolean")
		}
	}

	rawOut, _ := args["output_path"].(string)
	if strings.TrimSpace(rawOut) == "" {
		return nil
	}
	_, err := t.resolveOutputPath(rawOut)
	return err
}

// resolveOutputPath confines a supplied file name to the configured output
// directory or creates a unique timestamped target when empty.
func (t *TextToSFXTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.TextToSFX.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaOutputPath(dir, "sfx-", ".wav", raw)
}

// Execute executes the TextToSFX tool
func (t *TextToSFXTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	prompt, _ := args["prompt"].(string)
	rawOut, _ := args["output_path"].(string)

	var seconds *float32
	if raw, present := args["seconds"]; present && raw != nil {
		if f, ok := raw.(float64); ok {
			s := float32(f)
			seconds = &s
		}
	}
	var loop *bool
	if raw, present := args["loop"]; present && raw != nil {
		if b, ok := raw.(bool); ok {
			loop = &b
		}
	}

	start := time.Now()

	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	if err := t.sfx.Generate(ctx, prompt, outPath, seconds, loop); err != nil {
		if strings.TrimSpace(rawOut) == "" {
			_ = os.Remove(outPath)
		}
		return t.failure(start, args, err), nil
	}

	duration := 0.0
	if d, err := audio.WAVDurationSeconds(outPath); err == nil {
		duration = d
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToSFX",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":             outPath,
			"prompt":           prompt,
			"duration_seconds": duration,
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *TextToSFXTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToSFX",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled.
func (t *TextToSFXTool) IsEnabled() bool {
	return t.config.TextToSFX.Enabled && t.sfx != nil
}

// FormatPreview formats the result for display preview
func (t *TextToSFXTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "SFX generation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Sound effect generated"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Sound effect saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *TextToSFXTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("SFX generation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Sound effect generated"
	}
	path, _ := data["path"].(string)
	summary := fmt.Sprintf("Sound effect saved to %s", path)
	if d, ok := data["duration_seconds"].(float64); ok && d > 0 {
		summary = fmt.Sprintf("%s (%.1fs of audio)", summary, d)
	}
	formatter := agentinfra.NewBaseFormatter("TextToSFX")
	return formatter.FormatExpanded(result, summary)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TextToSFXTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TextToSFXTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *TextToSFXTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
