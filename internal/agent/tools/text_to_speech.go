package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	audio "github.com/inference-gateway/cli/internal/audio"
)

// voiceSynthesizer turns text into a spoken WAV file. It is defined here
// (consumer side) so tests can inject a fake; *audio.Synthesizer implements it.
type voiceSynthesizer interface {
	Synthesize(ctx context.Context, text, voiceSamplePath, outPath string) error
}

// TextToSpeechTool synthesizes speech from text with the configured local TTS
// engine and saves it to a WAV file. With a voice_sample it clones that voice
// (zero-shot); without one it uses the stock voice. It is opt-in via
// text_to_speech.enabled: when disabled the tool is not registered and its
// definition never reaches the LLM tools payload.
type TextToSpeechTool struct {
	config *config.Config
	synth  voiceSynthesizer
}

// NewTextToSpeechTool creates a new TextToSpeech tool.
func NewTextToSpeechTool(cfg *config.Config, synth voiceSynthesizer) *TextToSpeechTool {
	return &TextToSpeechTool{
		config: cfg,
		synth:  synth,
	}
}

// Definition returns the tool definition for TextToSpeech
func (t *TextToSpeechTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.TextToSpeech.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "TextToSpeech",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{
						"type":        "string",
						"description": "The text to speak",
					},
					"voice_sample": map[string]any{
						"type":        "string",
						"description": "Optional path to a WAV recording of the target speaker; when set, the output clones that voice. Around 10-30 seconds of clean single-speaker speech works best",
					},
					"output_path": map[string]any{
						"type":        "string",
						"description": "Optional path for the generated WAV file; defaults to a timestamped file under the configured output directory",
					},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates TextToSpeech arguments
func (t *TextToSpeechTool) Validate(args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		return fmt.Errorf("text is required and must be a non-empty string")
	}

	if sample, ok := args["voice_sample"].(string); ok && strings.TrimSpace(sample) != "" {
		if err := t.config.ValidatePathInSandbox(sample); err != nil {
			return err
		}
		safeSamplePath, err := filepath.Abs(sample)
		if err != nil {
			return fmt.Errorf("invalid voice_sample path %q: %w", sample, err)
		}
		f, err := os.Open(safeSamplePath) // nolint:gosec // path is validated against the sandbox above
		if err != nil {
			return fmt.Errorf("voice_sample %q must be an existing, readable WAV file: %w", safeSamplePath, err)
		}
		_ = f.Close()
	}

	if out, ok := args["output_path"].(string); ok && strings.TrimSpace(out) != "" {
		if err := t.config.ValidatePathInSandboxWrite(out); err != nil {
			return err
		}
	}

	return nil
}

// Execute executes the TextToSpeech tool
func (t *TextToSpeechTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	text, _ := args["text"].(string)
	voiceSample, _ := args["voice_sample"].(string)
	outputPath, _ := args["output_path"].(string)

	start := time.Now()

	outPath := strings.TrimSpace(outputPath)
	if outPath == "" {
		var err error
		outPath, err = t.defaultOutputPath()
		if err != nil {
			return &agentdomain.ToolExecutionResult{
				ToolName:  "TextToSpeech",
				Arguments: args,
				Success:   false,
				Duration:  time.Since(start),
				Error:     err.Error(),
			}, nil
		}
	}

	if err := t.synth.Synthesize(ctx, text, voiceSample, outPath); err != nil {
		return &agentdomain.ToolExecutionResult{
			ToolName:  "TextToSpeech",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	duration := 0.0
	if d, err := audio.WAVDurationSeconds(outPath); err == nil {
		duration = d
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToSpeech",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":             outPath,
			"text":             text,
			"duration_seconds": duration,
			"voice_cloned":     strings.TrimSpace(voiceSample) != "",
		},
	}, nil
}

// defaultOutputPath returns a timestamped WAV path under the configured
// output directory (~/.infer/tts by default), creating the directory.
func (t *TextToSpeechTool) defaultOutputPath() (string, error) {
	dir, err := t.config.TextToSpeech.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory: %w", err)
	}
	return filepath.Join(dir, "speech-"+time.Now().Format("20060102-150405.000")+".wav"), nil
}

// IsEnabled returns whether the tool is enabled: the feature flag must be on
// and the configured engine must be one this build supports. Because the
// registry only includes enabled tools in the chat-completion tools payload,
// a disabled tool costs zero prompt tokens.
func (t *TextToSpeechTool) IsEnabled() bool {
	if !t.config.TextToSpeech.Enabled || t.synth == nil {
		return false
	}
	engine := strings.TrimSpace(t.config.TextToSpeech.Engine)
	return engine == "" || engine == config.TextToSpeechEngineQwen3
}

// FormatPreview formats the result for display preview
func (t *TextToSpeechTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Speech synthesis failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Speech generated"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Speech saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *TextToSpeechTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Speech synthesis failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Speech generated"
	}
	path, _ := data["path"].(string)
	summary := fmt.Sprintf("Speech saved to %s", path)
	if d, ok := data["duration_seconds"].(float64); ok && d > 0 {
		summary = fmt.Sprintf("%s (%.1fs of audio)", summary, d)
	}
	formatter := agentinfra.NewBaseFormatter("TextToSpeech")
	return formatter.FormatExpanded(result, summary)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TextToSpeechTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TextToSpeechTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *TextToSpeechTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
