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

// TextToMusicTool composes music from a text prompt through the gateway's
// Music API and saves it as a WAV file.
type TextToMusicTool struct {
	config *config.Config
	music  agentdomain.MusicService
}

// NewTextToMusicTool creates a new TextToMusic tool.
func NewTextToMusicTool(cfg *config.Config, music agentdomain.MusicService) *TextToMusicTool {
	return &TextToMusicTool{
		config: cfg,
		music:  music,
	}
}

// Definition returns the tool definition for TextToMusic
func (t *TextToMusicTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.TextToMusic.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "TextToMusic",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Description of the music to compose - genre, mood, instruments, tempo",
					},
					"seconds": map[string]any{
						"type":        "number",
						"description": "Optional clip length in seconds; omitted lets the provider pick a length that fits the prompt",
					},
					"instrumental": map[string]any{
						"type":        "boolean",
						"description": "Optional: true to guarantee the generated clip has no vocals",
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

// Validate validates TextToMusic arguments
func (t *TextToMusicTool) Validate(args map[string]any) error {
	prompt, ok := args["prompt"].(string)
	if !ok || strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required and must be a non-empty string")
	}

	if raw, present := args["seconds"]; present && raw != nil {
		seconds, ok := raw.(float64)
		if !ok {
			return fmt.Errorf("seconds must be a number")
		}
		if seconds <= 0 {
			return fmt.Errorf("seconds must be a positive number")
		}
	}

	if raw, present := args["instrumental"]; present && raw != nil {
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("instrumental must be a boolean")
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
func (t *TextToMusicTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.TextToMusic.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaOutputPath(dir, "music-", raw)
}

// Execute executes the TextToMusic tool
func (t *TextToMusicTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
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
	var instrumental *bool
	if raw, present := args["instrumental"]; present && raw != nil {
		if b, ok := raw.(bool); ok {
			instrumental = &b
		}
	}

	start := time.Now()

	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	if err := t.music.Compose(ctx, prompt, outPath, seconds, instrumental); err != nil {
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
		ToolName:  "TextToMusic",
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
func (t *TextToMusicTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToMusic",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled.
func (t *TextToMusicTool) IsEnabled() bool {
	return t.config.TextToMusic.Enabled && t.music != nil
}

// FormatPreview formats the result for display preview
func (t *TextToMusicTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Music generation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Music generated"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Music saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *TextToMusicTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Music generation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Music generated"
	}
	path, _ := data["path"].(string)
	summary := fmt.Sprintf("Music saved to %s", path)
	if d, ok := data["duration_seconds"].(float64); ok && d > 0 {
		summary = fmt.Sprintf("%s (%.1fs of audio)", summary, d)
	}
	formatter := agentinfra.NewBaseFormatter("TextToMusic")
	return formatter.FormatExpanded(result, summary)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TextToMusicTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TextToMusicTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *TextToMusicTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
