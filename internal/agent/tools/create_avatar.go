package tools

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentinfra "github.com/inference-gateway/cli/internal/agent/infrastructure"
	avatars "github.com/inference-gateway/cli/internal/avatars"
)

// CreateAvatarTool builds a library avatar (~/.infer/avatars/<name>/) from a
// photo, generating extra angles with the ImageEdit model - the agent-side
// twin of `infer avatars create`, over the same avatars.Create. The photo is
// confined to the working directory and the session's artifacts directory.
type CreateAvatarTool struct {
	config       *config.Config
	imageService agentdomain.ImageService
}

// NewCreateAvatarTool creates a new CreateAvatar tool.
func NewCreateAvatarTool(cfg *config.Config, imageService agentdomain.ImageService) *CreateAvatarTool {
	return &CreateAvatarTool{
		config:       cfg,
		imageService: imageService,
	}
}

// Definition returns the tool definition for CreateAvatar
func (t *CreateAvatarTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.CreateAvatar.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "CreateAvatar",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Name of the new avatar: a bare folder name such as \"presenter\"; an existing name fails",
					},
					"photo": map[string]any{
						"type":        "string",
						"description": "Bare file name (no directories or absolute paths) of a front-facing .png, .jpg, .jpeg or .webp photo, looked up in the working directory, then in this session's artifacts directory",
					},
					"angles": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": slices.Sorted(maps.Keys(avatars.Angles))},
						"description": "Views to generate from the photo; defaults to both three-quarter views; [] stores the photo only",
					},
					"quality": map[string]any{
						"type":        "string",
						"enum":        imageEditQualities,
						"description": "Generated image quality",
						"default":     string(sdk.CreateImageEditMultipartBodyQualityHigh),
					},
					"size": map[string]any{
						"type":        "string",
						"description": "Generated image size as WIDTHxHEIGHT, or auto",
						"default":     string(sdk.ImageSize1024X1536),
					},
				},
				"required":             []string{"name", "photo"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate validates CreateAvatar arguments. The avatar name's form and an
// existing avatar are checked by avatars.Create before anything is written.
func (t *CreateAvatarTool) Validate(args map[string]any) error {
	if name, _ := args["name"].(string); strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	photo, _ := args["photo"].(string)
	photo = strings.TrimSpace(photo)
	if photo == "" {
		return fmt.Errorf("photo is required")
	}
	if filepath.IsAbs(photo) || strings.ContainsAny(photo, `/\`) || strings.Contains(photo, "..") {
		return fmt.Errorf("invalid photo path %q: pass a bare file name inside the working directory", photo)
	}
	if !slices.Contains(avatars.ImageExtensions, strings.ToLower(filepath.Ext(photo))) {
		return fmt.Errorf("photo %q must be a .png, .jpg, .jpeg or .webp image", photo)
	}
	if _, err := createAvatarAngles(args); err != nil {
		return err
	}
	if quality, ok := args["quality"].(string); ok && !slices.Contains(imageEditQualities, quality) {
		return fmt.Errorf("quality must be one of: %s", strings.Join(imageEditQualities, ", "))
	}
	return nil
}

// createAvatarAngles reads the optional angles argument: absent means the
// default views, an empty array means none.
func createAvatarAngles(args map[string]any) ([]string, error) {
	raw, ok := args["angles"]
	if !ok || raw == nil {
		return avatars.DefaultAngles, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("angles must be an array of view names")
	}
	angles := make([]string, 0, len(list))
	for _, item := range list {
		angle, _ := item.(string)
		if _, known := avatars.Angles[angle]; !known {
			return nil, fmt.Errorf("unknown angle %q (choose from %s)", angle, strings.Join(slices.Sorted(maps.Keys(avatars.Angles)), ", "))
		}
		angles = append(angles, angle)
	}
	return angles, nil
}

// Execute executes the CreateAvatar tool
func (t *CreateAvatarTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	if err := t.Validate(args); err != nil {
		return nil, err
	}

	name, _ := args["name"].(string)
	rawPhoto, _ := args["photo"].(string)
	angles, _ := createAvatarAngles(args)
	quality, _ := args["quality"].(string)
	size, _ := args["size"].(string)
	if quality == "" {
		quality = string(sdk.CreateImageEditMultipartBodyQualityHigh)
	}
	if size == "" {
		size = string(sdk.ImageSize1024X1536)
	}

	start := time.Now()
	photo, err := resolveMediaInputPath(t.config, t.config.SessionArtifactsDir(agentdomain.GetSessionID(ctx)), rawPhoto, "photo", "image file")
	if err != nil {
		return t.failure(start, args, err), nil
	}

	var edit avatars.EditFunc
	if len(angles) > 0 {
		model := t.config.Tools.ImageEdit.Model
		if !t.config.Tools.ImageEdit.Enabled || model == "" {
			return t.failure(start, args, fmt.Errorf("generating angles uses the ImageEdit model, which is disabled: ask the user to enable tools.image_edit, or pass angles [] to store the photo only")), nil
		}
		edit = func(ctx context.Context, prompt, imagePath string) (string, error) {
			return t.imageService.EditImage(ctx, model, prompt, imagePath, quality, size, "")
		}
	}

	avatar, err := avatars.Create(ctx, avatars.Dir(), strings.TrimSpace(name), photo, angles, edit)
	if err != nil {
		return t.failure(start, args, err), nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  "CreateAvatar",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"name":   avatar.Name,
			"images": avatar.Images,
		},
	}, nil
}

// failure builds the failed ToolExecutionResult for Execute.
func (t *CreateAvatarTool) failure(start time.Time, args map[string]any, err error) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName:  "CreateAvatar",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}
}

// IsEnabled reports whether the feature is enabled: TextToVideo and its
// separate create_avatar opt-in.
func (t *CreateAvatarTool) IsEnabled() bool {
	return t.config.TextToVideo.Enabled && t.config.TextToVideo.CreateAvatar && t.imageService != nil
}

// FormatPreview formats the result for display preview
func (t *CreateAvatarTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Avatar creation failed"
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Avatar created"
	}
	name, _ := data["name"].(string)
	images, _ := data["images"].([]string)
	return fmt.Sprintf("Created avatar %s (%d images)", name, len(images))
}

// FormatForLLM formats the result for LLM consumption
func (t *CreateAvatarTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Error: no result"
	}
	if !result.Success {
		return fmt.Sprintf("Avatar creation failed: %s", result.Error)
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return "Avatar created"
	}
	name, _ := data["name"].(string)
	images, _ := data["images"].([]string)
	summary := fmt.Sprintf("Created avatar %s with images %s; pass avatar %q to TextToVideo", name, strings.Join(images, ", "), name)
	formatter := agentinfra.NewBaseFormatter("CreateAvatar")
	return formatter.FormatExpanded(result, summary)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *CreateAvatarTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *CreateAvatarTool) ShouldAlwaysExpand() bool {
	return false
}

// FormatResult formats the result based on the requested format type
func (t *CreateAvatarTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}
