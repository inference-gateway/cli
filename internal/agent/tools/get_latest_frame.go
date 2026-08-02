package tools

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// frameSourceLookup is the narrow slice of the Registry the tool needs.
type frameSourceLookup interface {
	FrameSource(name string) (domain.FrameSource, bool)
	FrameSourceNames() []string
}

// GetLatestFrameTool retrieves the latest frame from a named frame source
// (the screen ring buffer or a configured directory source), either as a raw
// image or as annotated text for models without vision.
type GetLatestFrameTool struct {
	config          *config.Config
	formatter       domain.BaseFormatter
	sources         frameSourceLookup
	annotator       domain.ImageAnnotator
	lastCallMu      sync.Mutex
	lastCallTimes   map[string]time.Time
	minCallInterval time.Duration
}

// NewGetLatestFrameTool creates a new tool that reads from the frame sources
func NewGetLatestFrameTool(cfg *config.Config, sources frameSourceLookup, annotator domain.ImageAnnotator) *GetLatestFrameTool {
	minInterval := time.Duration(cfg.ComputerUse.Screenshot.CaptureInterval) * time.Second
	if minInterval < 2*time.Second {
		minInterval = 2 * time.Second
	}

	return &GetLatestFrameTool{
		config:          cfg,
		formatter:       domain.NewBaseFormatter("GetLatestFrame"),
		sources:         sources,
		annotator:       annotator,
		lastCallTimes:   make(map[string]time.Time),
		minCallInterval: minInterval,
	}
}

// Definition returns the tool definition for the LLM
func (t *GetLatestFrameTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.GetLatestFrame.Description
	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "GetLatestFrame",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"source": map[string]any{
						"type":        "string",
						"description": fmt.Sprintf("Frame source name. Configured sources: %s. Defaults to the only source, or \"screen\" when present.", strings.Join(t.sources.FrameSourceNames(), ", ")),
					},
					"format": map[string]any{
						"type":        "string",
						"enum":        []string{"regular", "annotated"},
						"description": "\"regular\" returns the raw image; \"annotated\" returns a text summary + element list. Omit to choose automatically.",
					},
				},
			},
		},
	}
}

// Execute retrieves the latest frame from the requested source
func (t *GetLatestFrameTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	fail := func(msg string) (*domain.ToolExecutionResult, error) {
		return &domain.ToolExecutionResult{
			ToolName:  "GetLatestFrame",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     msg,
		}, nil
	}

	sourceName, src, errMsg := t.resolveSource(args)
	if errMsg != "" {
		return fail(errMsg)
	}

	if waitMsg := t.checkRateLimit(sourceName); waitMsg != "" {
		return fail(waitMsg)
	}

	frame, err := src.GetLatestFrame()
	if err != nil {
		return fail(err.Error())
	}

	attachment := domain.ImageAttachment{
		Data:        frame.Data,
		MimeType:    "image/" + frame.Format,
		DisplayName: "frame-" + sourceName,
		SourcePath:  frame.Path,
	}
	result := domain.FrameToolResult{
		Source: sourceName,
		Width:  frame.Width,
		Height: frame.Height,
		Format: frame.Format,
		Method: frame.Method,
	}

	textOnly := t.config.Vision.IsTextOnlyModel(domain.GetModel(ctx))
	if t.wantAnnotated(args, textOnly) {
		return t.annotatedResult(ctx, args, sourceName, attachment, result, textOnly, start)
	}

	return &domain.ToolExecutionResult{
		ToolName:  "GetLatestFrame",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
		Images:    []domain.ImageAttachment{attachment},
	}, nil
}

// resolveSource picks the frame source: explicit name, the only one
// configured, or "screen" when several exist.
func (t *GetLatestFrameTool) resolveSource(args map[string]any) (string, domain.FrameSource, string) {
	names := t.sources.FrameSourceNames()
	if len(names) == 0 {
		return "", nil, "no frame sources available: enable computer_use screenshot streaming or configure vision.sources"
	}

	name, _ := args["source"].(string)
	switch {
	case name != "":
	case len(names) == 1:
		name = names[0]
	default:
		name = "screen"
	}

	src, ok := t.sources.FrameSource(name)
	if !ok {
		return "", nil, fmt.Sprintf("unknown frame source %q: available sources are %s", name, strings.Join(names, ", "))
	}
	return name, src, ""
}

// checkRateLimit enforces the per-source minimum call interval; it returns a
// wait message when the call is too soon.
func (t *GetLatestFrameTool) checkRateLimit(source string) string {
	t.lastCallMu.Lock()
	defer t.lastCallMu.Unlock()
	if last, ok := t.lastCallTimes[source]; ok {
		if since := time.Since(last); since < t.minCallInterval {
			wait := t.minCallInterval - since
			return fmt.Sprintf("please wait %v before requesting another frame from %q (last called %v ago)", wait.Round(time.Second), source, since.Round(time.Second))
		}
	}
	t.lastCallTimes[source] = time.Now()
	return ""
}

// wantAnnotated resolves the format: explicit wins; omitted defaults to
// annotated only for text-only session models with a configured annotator.
func (t *GetLatestFrameTool) wantAnnotated(args map[string]any, textOnly bool) bool {
	switch format, _ := args["format"].(string); format {
	case "regular":
		return false
	case "annotated":
		return true
	default:
		return t.annotator != nil && textOnly
	}
}

// annotatedResult annotates the frame and builds the result. Annotation
// problems never fail the call: they degrade to a regular frame (vision
// models) or an omission note (text-only models).
func (t *GetLatestFrameTool) annotatedResult(ctx context.Context, args map[string]any, sourceName string, attachment domain.ImageAttachment, result domain.FrameToolResult, textOnly bool, start time.Time) (*domain.ToolExecutionResult, error) {
	images := []domain.ImageAttachment{attachment}
	if textOnly {
		images = nil // never send base64 to a model that cannot see it
	}

	degrade := func(note string) (*domain.ToolExecutionResult, error) {
		result.Note = note
		return &domain.ToolExecutionResult{
			ToolName:  "GetLatestFrame",
			Arguments: args,
			Success:   true,
			Duration:  time.Since(start),
			Data:      result,
			Images:    images,
		}, nil
	}

	if t.annotator == nil {
		if textOnly {
			return degrade("[image omitted: model has no vision; configure vision.annotator to enable annotated frames]")
		}
		return degrade("annotation unavailable: set vision.annotator in config.yaml; returned the regular frame")
	}

	annotation, err := t.annotator.AnnotateImage(ctx, attachment, domain.AnnotateOptions{
		Prompt: t.annotationPrompt(sourceName),
		Width:  result.Width,
		Height: result.Height,
	})
	if err != nil {
		if textOnly {
			return degrade(fmt.Sprintf("[image omitted: model has no vision; annotation failed: %v]", err))
		}
		return degrade(fmt.Sprintf("annotation failed: %v; returned the regular frame", err))
	}

	result.Annotated = true
	result.Annotation = annotation
	return &domain.ToolExecutionResult{
		ToolName:  "GetLatestFrame",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      result,
		Images:    images,
	}, nil
}

// annotationPrompt resolves the task prompt for a source: per-source override,
// the screen prompt for the screen source, the scene prompt otherwise.
func (t *GetLatestFrameTool) annotationPrompt(source string) string {
	if src, ok := t.config.Vision.Sources[source]; ok && strings.TrimSpace(src.Prompt) != "" {
		return src.Prompt
	}
	if source == "screen" {
		return t.config.Prompts.Vision.Annotator.ScreenSystemPrompt
	}
	return t.config.Prompts.Vision.Annotator.SceneSystemPrompt
}

// Validate checks if the tool arguments are valid
func (t *GetLatestFrameTool) Validate(args map[string]any) error {
	if format, ok := args["format"].(string); ok && format != "" && format != "regular" && format != "annotated" {
		return fmt.Errorf("invalid format %q: must be \"regular\" or \"annotated\"", format)
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *GetLatestFrameTool) IsEnabled() bool {
	return len(t.sources.FrameSourceNames()) > 0
}

// FormatResult formats tool execution results for different contexts
func (t *GetLatestFrameTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForLLM(result)
	}
}

// FormatPreview returns a short preview of the result for UI display
func (t *GetLatestFrameTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return "Failed to get latest frame"
	}
	data, ok := result.Data.(domain.FrameToolResult)
	if !ok {
		return "Latest frame retrieved"
	}
	if data.Annotated {
		return fmt.Sprintf("Annotated frame from %s: %d element(s)", data.Source, len(data.Annotation.Elements))
	}
	return fmt.Sprintf("Latest frame from %s: %dx%d (%s)", data.Source, data.Width, data.Height, data.Method)
}

// FormatForLLM formats the result for LLM consumption
func (t *GetLatestFrameTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil || !result.Success {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	data, ok := result.Data.(domain.FrameToolResult)
	if !ok {
		return "Latest frame retrieved successfully. Image is attached."
	}

	if data.Annotated {
		text := domain.AnnotationText(data.Annotation)
		if data.Source == "screen" && len(data.Annotation.Elements) > 0 {
			text += "\nTo interact: pass an element's center (x,y) to MouseClick."
		}
		return text
	}

	msg := fmt.Sprintf("Latest frame from source %q retrieved successfully (%dx%d, format: %s, method: %s).", data.Source, data.Width, data.Height, data.Format, data.Method)
	if data.Note != "" {
		return msg + " " + data.Note
	}
	return msg + " Image is attached."
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *GetLatestFrameTool) ShouldCollapseArg(key string) bool {
	return false
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *GetLatestFrameTool) ShouldAlwaysExpand() bool {
	return false
}
