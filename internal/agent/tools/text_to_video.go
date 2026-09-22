package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
)

// avatarExtensions are the portrait formats the desktop avatar library holds.
var avatarExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// audioExtensions are the driving-audio formats the gateway forwards to
// avatar models: TextToSpeech's WAV output and any MP3 clip.
var audioExtensions = []string{".wav", ".mp3"}

// TextToVideoTool renders a video clip from a text prompt through the
// gateway's Videos API and saves it as an MP4 file; with a portrait and an
// audio clip it renders a lip-synced talking clip.
type TextToVideoTool struct {
	config *config.Config
	video  agentdomain.VideoService
}

// NewTextToVideoTool creates a new TextToVideo tool.
func NewTextToVideoTool(cfg *config.Config, video agentdomain.VideoService) *TextToVideoTool {
	return &TextToVideoTool{
		config: cfg,
		video:  video,
	}
}

// Definition returns the tool definition for TextToVideo
func (t *TextToVideoTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.TextToVideo.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "TextToVideo",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "Description of the shot to render; required unless audio is provided, and for avatar renders it describes framing only - the dialogue comes from the audio clip",
					},
					"seconds": map[string]any{
						"type":        "string",
						"description": "Optional clip length in seconds as a string (e.g. \"4\"); providers accept a limited set of values; ignored when audio is present",
					},
					"size": map[string]any{
						"type":        "string",
						"description": "Optional output resolution as widthxheight (e.g. 720x1280 portrait or 1280x720 landscape); passed through verbatim, the provider derives the resolution from the shorter side (480, 720 or 1080); omitted means the provider default",
					},
					"avatar": map[string]any{
						"type":        "string",
						"description": "Optional bare file name (no directories or absolute paths) of a portrait image (.png, .jpg, .jpeg or .webp); looked up in the working directory, then in the avatar library (~/.infer/models/avatars); with audio the portrait is lip-synced to the clip",
					},
					"audio": map[string]any{
						"type":        "string",
						"description": "Optional bare file name (no directories or absolute paths) of a .wav or .mp3 clip that drives the render; requires avatar; looked up in the working directory, then in the TextToSpeech output directory",
					},
					"output_path": map[string]any{
						"type":        "string",
						"description": "Optional bare file name (no directories or absolute paths) for the generated MP4; it is always placed in the configured output directory. Defaults to a timestamped file",
					},
				},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates TextToVideo arguments
func (t *TextToVideoTool) Validate(args map[string]any) error {
	prompt, _ := args["prompt"].(string)
	rawAvatar, _ := args["avatar"].(string)
	rawAudio, _ := args["audio"].(string)

	if strings.TrimSpace(rawAudio) == "" && strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("prompt is required unless audio is provided")
	}
	if strings.TrimSpace(rawAudio) != "" && strings.TrimSpace(rawAvatar) == "" {
		return fmt.Errorf("audio requires avatar: pass a portrait image name for the lip-synced clip")
	}

	if _, err := t.resolveAvatarPath(rawAvatar); err != nil {
		return err
	}
	if _, err := t.resolveAudioPath(rawAudio); err != nil {
		return err
	}

	rawOut, _ := args["output_path"].(string)
	if strings.TrimSpace(rawOut) == "" {
		return nil
	}
	_, err := t.resolveOutputPath(rawOut)
	return err
}

// resolveAvatarPath confines an optional portrait to a readable image in the
// working directory, falling back to the desktop avatar library
// (~/.infer/models/avatars), and returns an empty path when unset.
func (t *TextToVideoTool) resolveAvatarPath(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	if ext := strings.ToLower(filepath.Ext(name)); !slices.Contains(avatarExtensions, ext) {
		return "", fmt.Errorf("avatar %q must be a .png, .jpg, .jpeg or .webp image", raw)
	}
	dir, _ := avatarLibraryDir() // an unusable library just drops the fallback
	return resolveMediaInputPath(t.config, dir, raw, "avatar", "image file")
}

// resolveAudioPath confines an optional driving clip to a readable .wav or
// .mp3 file in the working directory, falling back to the TextToSpeech
// output directory, and returns an empty path when unset.
func (t *TextToVideoTool) resolveAudioPath(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", nil
	}
	if ext := strings.ToLower(filepath.Ext(name)); !slices.Contains(audioExtensions, ext) {
		return "", fmt.Errorf("audio %q must be a .wav or .mp3 file", raw)
	}
	dir, _ := t.config.TextToSpeech.ResolveOutputDir() // an unusable dir just drops the fallback
	return resolveMediaInputPath(t.config, dir, raw, "audio", ".wav or .mp3 file")
}

// resolveOutputPath confines a supplied file name to the configured output
// directory or creates a unique timestamped target when empty.
func (t *TextToVideoTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.TextToVideo.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaOutputPath(dir, "video-", ".mp4", raw)
}

// avatarLibraryDir returns the desktop-managed avatar library
// (~/.infer/models/avatars) holding portrait images for lip-synced renders.
func avatarLibraryDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "models", "avatars"), nil
}

// Execute executes the TextToVideo tool
func (t *TextToVideoTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	prompt, _ := args["prompt"].(string)
	rawAvatar, _ := args["avatar"].(string)
	rawAudio, _ := args["audio"].(string)
	rawOut, _ := args["output_path"].(string)
	seconds, _ := args["seconds"].(string)
	size, _ := args["size"].(string)

	start := time.Now()

	avatarPath, err := t.resolveAvatarPath(rawAvatar)
	if err != nil {
		return t.failure(start, args, err), nil
	}
	audioPath, err := t.resolveAudioPath(rawAudio)
	if err != nil {
		return t.failure(start, args, err), nil
	}
	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	request := agentdomain.VideoRequest{
		Prompt:     prompt,
		Seconds:    seconds,
		Size:       size,
		AvatarPath: avatarPath,
		AudioPath:  audioPath,
	}
	if err := t.video.Render(ctx, request, outPath); err != nil {
		if strings.TrimSpace(rawOut) == "" {
			_ = os.Remove(outPath)
		}
		return t.failure(start, args, err), nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToVideo",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":    outPath,
			"model":   t.config.TextToVideo.ResolveGatewayModel(),
			"seconds": seconds,
			"size":    size,
			"avatar":  strings.TrimSpace(rawAvatar),
			"audio":   strings.TrimSpace(rawAudio),
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *TextToVideoTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "TextToVideo",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled.
func (t *TextToVideoTool) IsEnabled() bool {
	return t.config.TextToVideo.Enabled && t.video != nil
}

// FormatPreview formats the result for display preview
func (t *TextToVideoTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Video generation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Video generated"
	}
	path, _ := data["path"].(string)
	return fmt.Sprintf("Video saved to %s", path)
}

// FormatForLLM formats the result for LLM consumption
func (t *TextToVideoTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Video generation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Video generated"
	}
	path, _ := data["path"].(string)
	summary := fmt.Sprintf("Video saved to %s", path)
	formatter := agentinfra.NewBaseFormatter("TextToVideo")
	return formatter.FormatExpanded(result, summary)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *TextToVideoTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *TextToVideoTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *TextToVideoTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
