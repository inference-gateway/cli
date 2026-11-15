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
)

// Argument names of the DubVideo manifest.
const (
	dubVideoArgVideo          = "video"
	dubVideoArgPrompt         = "prompt"
	dubVideoArgPreserveSpeech = "preserve_speech"
	dubVideoArgOutputPath     = "output_path"
)

// dubVideoResultModel is the result-data key naming the model that dubbed the clip.
const dubVideoResultModel = "model"

// A dubbed clip without a named output is saved as dub-<timestamp>.mp4.
const (
	dubbedFilePrefix    = "dub-"
	dubbedFileExtension = ".mp4"
)

// DubVideoTool dubs a video clip through the gateway's Videos API: the
// provider generates the sound that fits the clip and re-renders it with that
// sound as its new soundtrack, saved as an MP4 file.
type DubVideoTool struct {
	config *config.Config
	video  agentdomain.VideoService
}

// NewDubVideoTool creates a new DubVideo tool.
func NewDubVideoTool(cfg *config.Config, video agentdomain.VideoService) *DubVideoTool {
	return &DubVideoTool{
		config: cfg,
		video:  video,
	}
}

// Manifest returns the tool's manifest.
func (t *DubVideoTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolDubVideo)
}

// Definition returns the tool definition for DubVideo
func (t *DubVideoTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Validate validates DubVideo arguments
func (t *DubVideoTool) Validate(args map[string]any) error {
	rawVideo, _ := args[dubVideoArgVideo].(string)
	if strings.TrimSpace(rawVideo) == "" {
		return fmt.Errorf("%s is required", dubVideoArgVideo)
	}
	if ext := strings.ToLower(filepath.Ext(rawVideo)); !slices.Contains(videoExtensions, ext) {
		return fmt.Errorf("%s %q must be one of %s", dubVideoArgVideo, rawVideo, strings.Join(videoExtensions, ", "))
	}

	if raw, present := args[dubVideoArgPreserveSpeech]; present && raw != nil {
		if _, ok := raw.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", dubVideoArgPreserveSpeech)
		}
	}

	if _, err := t.resolveVideoPath(rawVideo); err != nil {
		return err
	}

	rawOut, _ := args[dubVideoArgOutputPath].(string)
	if strings.TrimSpace(rawOut) == "" {
		return nil
	}
	_, err := t.resolveOutputPath(rawOut)
	return err
}

// resolveVideoPath confines the clip to a readable video file in the working
// directory, falling back to the TextToVideo output directory.
func (t *DubVideoTool) resolveVideoPath(raw string) (string, error) {
	dir, err := t.config.TextToVideo.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaInputPath(t.config, dir, raw, dubVideoArgVideo, strings.Join(videoExtensions, "/")+" clip")
}

// resolveOutputPath confines a supplied file name to the configured output
// directory or creates a unique timestamped target when empty.
func (t *DubVideoTool) resolveOutputPath(raw string) (string, error) {
	dir, err := t.config.TextToVideo.ResolveOutputDir()
	if err != nil {
		return "", err
	}
	return resolveMediaOutputPath(dir, dubbedFilePrefix, dubbedFileExtension, raw)
}

// Execute executes the DubVideo tool
func (t *DubVideoTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	rawVideo, _ := args[dubVideoArgVideo].(string)
	prompt, _ := args[dubVideoArgPrompt].(string)
	rawOut, _ := args[dubVideoArgOutputPath].(string)

	start := time.Now()

	videoPath, err := t.resolveVideoPath(rawVideo)
	if err != nil {
		return t.failure(start, args, err), nil
	}
	outPath, err := t.resolveOutputPath(rawOut)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	request := agentdomain.VideoDubRequest{Prompt: prompt, VideoPath: videoPath}
	if preserve, ok := args[dubVideoArgPreserveSpeech].(bool); ok {
		request.PreserveSpeech = &preserve
	}
	if err := t.video.Dub(ctx, request, outPath); err != nil {
		if strings.TrimSpace(rawOut) == "" {
			_ = os.Remove(outPath)
		}
		return t.failure(start, args, err), nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolDubVideo,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			savedPathKey:              outPath,
			dubVideoResultModel:       t.config.VideoToSFX.ResolveGatewayModel(),
			dubVideoArgVideo:          strings.TrimSpace(rawVideo),
			dubVideoArgPreserveSpeech: request.PreserveSpeech != nil && *request.PreserveSpeech,
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *DubVideoTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolDubVideo,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled: the video_to_sfx section
// and its separate dub opt-in, the way CreateAvatar sits under text_to_video.
func (t *DubVideoTool) IsEnabled() bool {
	return t.config.VideoToSFX.DubEnabled() && t.video != nil
}

// FormatPreview formats the result for display preview
func (t *DubVideoTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterShort, dubbedVideoFile)
}

// FormatForLLM formats the result for LLM consumption
func (t *DubVideoTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	return formatSavedResult(result, agentdomain.FormatterLLM, dubbedVideoFile)
}

// FormatResult formats the result based on the requested format type
func (t *DubVideoTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	return formatSavedResult(result, formatType, dubbedVideoFile)
}

// dubbedVideoFile describes the file the DubVideo tool saves.
var dubbedVideoFile = savedFile{
	tool:    ToolDubVideo,
	noun:    "Video",
	failure: "Video dubbing failed",
	done:    "Video dubbed",
}
