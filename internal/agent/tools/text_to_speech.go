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

// TextToSpeechTool synthesizes speech with the configured local engine and
// saves it to a WAV file, optionally cloning a supplied voice sample.
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
						"description": "Optional file name (inside the working directory) of a WAV recording of the target speaker; when set, the output clones that voice. Around 10-30 seconds of clean single-speaker speech works best",
					},
					"output_path": map[string]any{
						"type":        "string",
						"description": "Optional file name for the generated WAV file, placed in the configured output directory; defaults to a timestamped file",
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

	rawSample, _ := args["voice_sample"].(string)
	if _, err := t.resolveSamplePath(rawSample); err != nil {
		return err
	}

	rawOut, _ := args["output_path"].(string)
	if strings.TrimSpace(rawOut) == "" {
		return nil
	}
	_, err := t.resolveOutputPath(rawOut)
	return err
}

// resolveSamplePath confines an optional voice sample to a readable file in
// the working directory and returns an empty path for the stock voice.
func (t *TextToSpeechTool) resolveSamplePath(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid voice_sample path %q: pass a bare file name inside the working directory", raw)
	}

	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving working directory: %w", err)
	}
	safePath := filepath.Join(workDir, filepath.Base(name))

	if err := t.config.ValidatePathInSandbox(safePath); err != nil {
		return "", err
	}
	info, err := os.Stat(safePath)
	if err != nil {
		return "", fmt.Errorf("voice_sample %q must be an existing WAV file: %w", safePath, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("voice_sample %q is a directory, not a WAV file", safePath)
	}
	f, err := os.Open(safePath) // nolint:gosec
	if err != nil {
		return "", fmt.Errorf("voice_sample %q must be readable: %w", safePath, err)
	}
	_ = f.Close()
	return safePath, nil
}

// resolveOutputPath confines a supplied file name to the configured output
// directory or creates a unique timestamped target when empty.
func (t *TextToSpeechTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.TextToSpeech.ResolveOutputDir()
	if err != nil {
		return "", err
	}

	name := strings.TrimSpace(raw)
	if name == "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("creating output directory: %w", err)
		}
		file, err := os.CreateTemp(dir, "speech-"+time.Now().Format("20060102-150405")+"-*.wav")
		if err != nil {
			return "", fmt.Errorf("allocating output file: %w", err)
		}
		path := file.Name()
		if err := file.Close(); err != nil {
			_ = os.Remove(path)
			return "", fmt.Errorf("closing output file: %w", err)
		}
		return path, nil
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", fmt.Errorf("invalid output_path %q: pass a bare file name for the output directory", raw)
	}
	return filepath.Join(dir, filepath.Base(name)), nil
}

// Execute executes the TextToSpeech tool
func (t *TextToSpeechTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	text, _ := args["text"].(string)
	rawSample, _ := args["voice_sample"].(string)
	rawOut, _ := args["output_path"].(string)

	start := time.Now()

	sample, err := t.resolveSamplePath(rawSample)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	if err := t.synth.Synthesize(ctx, text, sample, outPath); err != nil {
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
		ToolName:  "TextToSpeech",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":             outPath,
			"text":             text,
			"duration_seconds": duration,
			"voice_cloned":     sample != "",
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *TextToSpeechTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToSpeech",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled with a supported engine.
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
