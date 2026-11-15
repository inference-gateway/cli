package tools

import (
	"cmp"
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
)

// videoExtensions are the clip formats the gateway's SFX video branch scores.
var videoExtensions = []string{".mov", ".mp4", ".m4v", ".webm"}

// VideoToSFXTool scores a video clip through the gateway's SFX video branch
// and saves the answered track as an audio file.
type VideoToSFXTool struct {
	config   *config.Config
	videoSfx agentdomain.VideoSoundEffectService
}

// NewVideoToSFXTool creates a new VideoToSFX tool.
func NewVideoToSFXTool(cfg *config.Config, videoSfx agentdomain.VideoSoundEffectService) *VideoToSFXTool {
	return &VideoToSFXTool{
		config:   cfg,
		videoSfx: videoSfx,
	}
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *VideoToSFXTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolVideoToSFX).WithRequireApproval(t.config.VideoToSFX.RequireApproval)
}

// Definition returns the tool definition for VideoToSFX
func (t *VideoToSFXTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Validate validates VideoToSFX arguments
func (t *VideoToSFXTool) Validate(args map[string]any) error {
	video, ok := args["video"].(string)
	if !ok || strings.TrimSpace(video) == "" {
		return fmt.Errorf("video is required and must be a bare file name")
	}
	if ext := strings.ToLower(filepath.Ext(strings.TrimSpace(video))); !slices.Contains(videoExtensions, ext) {
		return fmt.Errorf("video %q must be a .mov, .mp4, .m4v or .webm clip", video)
	}

	if raw, present := args["seconds"]; present && raw != nil {
		seconds, ok := raw.(float64)
		if !ok {
			return fmt.Errorf("seconds must be a number")
		}
		if seconds < 1 {
			return fmt.Errorf("seconds must be at least 1")
		}
	}

	if raw, present := args["start_seconds"]; present && raw != nil {
		start, ok := raw.(float64)
		if !ok {
			return fmt.Errorf("start_seconds must be a number")
		}
		if start < 0 {
			return fmt.Errorf("start_seconds must not be negative")
		}
	}

	if raw, present := args["track"]; present && raw != nil {
		track, ok := raw.(string)
		if !ok {
			return fmt.Errorf("track must be a string")
		}
		switch track {
		case "", "mix", "sfx", "speech":
		default:
			return fmt.Errorf("track must be one of mix, sfx or speech")
		}
	}

	if raw, present := args["preserve_speech"]; present && raw != nil {
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("preserve_speech must be a boolean")
		}
	}

	rawOut, _ := args["output_path"].(string)
	if strings.TrimSpace(rawOut) == "" {
		return nil
	}
	_, err := t.resolveOutputPath(rawOut)
	return err
}

// resolveVideoPath confines a supplied clip to a readable movie file in the
// working directory, falling back to the TextToVideo output directory.
func (t *VideoToSFXTool) resolveVideoPath(raw string) (string, error) {
	dir, _ := t.config.TextToVideo.ResolveOutputDir()
	return resolveMediaInputPath(t.config, dir, raw, "video", ".mov/.mp4/.m4v/.webm clip")
}

// resolveOutputPath confines a supplied file name to the configured output
// directory or creates a unique timestamped target when empty.
func (t *VideoToSFXTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.VideoToSFX.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaOutputPath(dir, "sfx-", ".mp3", raw)
}

// Execute executes the VideoToSFX tool
func (t *VideoToSFXTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	video, _ := args["video"].(string)
	prompt, _ := args["prompt"].(string)
	track, _ := args["track"].(string)
	rawOut, _ := args["output_path"].(string)

	var seconds *float32
	if raw, present := args["seconds"]; present && raw != nil {
		if f, ok := raw.(float64); ok {
			s := float32(f)
			seconds = &s
		}
	}
	var startSeconds *float32
	if raw, present := args["start_seconds"]; present && raw != nil {
		if f, ok := raw.(float64); ok {
			s := float32(f)
			startSeconds = &s
		}
	}
	var preserveSpeech *bool
	if raw, present := args["preserve_speech"]; present && raw != nil {
		if b, ok := raw.(bool); ok {
			preserveSpeech = &b
		}
	}

	start := time.Now()

	videoPath, err := t.resolveVideoPath(video)
	if err != nil {
		return t.failure(start, args, err), nil
	}
	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	request := agentdomain.VideoSoundEffectRequest{
		VideoPath:      videoPath,
		Prompt:         prompt,
		Seconds:        seconds,
		StartSeconds:   startSeconds,
		Track:          track,
		PreserveSpeech: preserveSpeech,
	}
	saved, err := t.videoSfx.Score(ctx, request, outPath)
	if err != nil {
		if strings.TrimSpace(rawOut) == "" {
			_ = os.Remove(outPath)
		}
		return t.failure(start, args, err), nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolVideoToSFX,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"path":   saved,
			"video":  filepath.Base(videoPath),
			"prompt": prompt,
			"track":  cmp.Or(track, "mix"),
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *VideoToSFXTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolVideoToSFX,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled.
func (t *VideoToSFXTool) IsEnabled() bool {
	return t.config.VideoToSFX.Enabled && t.videoSfx != nil
}

// FormatPreview formats the result for display preview
func (t *VideoToSFXTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterShort, videoScoreFile)
}

// FormatForLLM formats the result for LLM consumption
func (t *VideoToSFXTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterLLM, videoScoreFile)
}

// FormatResult formats the result based on the requested format type
func (t *VideoToSFXTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatSavedResult(result, formatType, videoScoreFile)
}

// videoScoreFile describes the file the VideoToSFX tool saves.
var videoScoreFile = savedFile{
	tool:    ToolVideoToSFX,
	noun:    "Sound effect",
	failure: "Video scoring failed",
	done:    "Video scored",
}
