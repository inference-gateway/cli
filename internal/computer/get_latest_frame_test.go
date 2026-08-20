package computer

import (
	"context"
	"errors"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"

	config "github.com/inference-gateway/cli/config"
)

// fakeSourceLookup is a hand-rolled frameSourceLookup over a plain map.
type fakeSourceLookup map[string]agentdomain.FrameSource

func (f fakeSourceLookup) FrameSource(name string) (agentdomain.FrameSource, bool) {
	src, ok := f[name]
	return src, ok
}

func (f fakeSourceLookup) FrameSourceNames() []string {
	names := make([]string, 0, len(f))
	for name := range f {
		names = append(names, name)
	}
	return names
}

func frameFixture() *agentdomain.Frame {
	return &agentdomain.Frame{ID: "f1", Data: "aW1n", Width: 1024, Height: 768, Format: "jpeg", Method: "x11", Path: "/tmp/f1.jpeg"}
}

func newFrameTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	return cfg
}

func TestGetLatestFrameIsEnabled(t *testing.T) {
	cfg := newFrameTestConfig()
	assert.False(t, NewGetLatestFrameTool(cfg, fakeSourceLookup{}, nil).IsEnabled())
	assert.True(t, NewGetLatestFrameTool(cfg, fakeSourceLookup{"screen": &agentdomainmocks.FakeFrameSource{}}, nil).IsEnabled())
}

func TestGetLatestFrameSourceResolution(t *testing.T) {
	screen := &agentdomainmocks.FakeFrameSource{}
	screen.GetLatestFrameReturns(frameFixture(), nil)
	cam := &agentdomainmocks.FakeFrameSource{}
	cam.GetLatestFrameReturns(&agentdomain.Frame{ID: "c1", Data: "aW1n", Width: 640, Height: 480, Format: "png", Method: "directory"}, nil)

	tests := []struct {
		name       string
		sources    fakeSourceLookup
		args       map[string]any
		wantOK     bool
		wantSource string
		wantErrSub string
	}{
		{"no sources", fakeSourceLookup{}, map[string]any{}, false, "", "no frame sources available"},
		{"single source auto", fakeSourceLookup{"cam": cam}, map[string]any{}, true, "cam", ""},
		{"multiple prefers screen", fakeSourceLookup{"screen": screen, "cam": cam}, map[string]any{}, true, "screen", ""},
		{"explicit source", fakeSourceLookup{"screen": screen, "cam": cam}, map[string]any{"source": "cam"}, true, "cam", ""},
		{"unknown source", fakeSourceLookup{"screen": screen}, map[string]any{"source": "nope"}, false, "", "unknown frame source"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := NewGetLatestFrameTool(newFrameTestConfig(), tt.sources, nil)
			result, err := tool.Execute(context.Background(), tt.args)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantOK, result.Success)
			if tt.wantOK {
				data := result.Data.(agentdomain.FrameToolResult)
				assert.Equal(t, tt.wantSource, data.Source)
				assert.Len(t, result.Images, 1)
			} else {
				assert.Contains(t, result.Error, tt.wantErrSub)
			}
		})
	}
}

func TestGetLatestFrameRateLimitPerSource(t *testing.T) {
	screen := &agentdomainmocks.FakeFrameSource{}
	screen.GetLatestFrameReturns(frameFixture(), nil)
	cam := &agentdomainmocks.FakeFrameSource{}
	cam.GetLatestFrameReturns(frameFixture(), nil)

	tool := NewGetLatestFrameTool(newFrameTestConfig(), fakeSourceLookup{"screen": screen, "cam": cam}, nil)

	first, _ := tool.Execute(context.Background(), map[string]any{"source": "screen"})
	assert.True(t, first.Success)

	second, _ := tool.Execute(context.Background(), map[string]any{"source": "screen"})
	assert.False(t, second.Success)
	assert.Contains(t, second.Error, "please wait")

	other, _ := tool.Execute(context.Background(), map[string]any{"source": "cam"})
	assert.True(t, other.Success, "a different source must not share the rate limit")
}

func TestGetLatestFrameFormatMatrix(t *testing.T) {
	annotation := &agentdomain.ImageAnnotation{
		Summary:  "Login form",
		Elements: []agentdomain.AnnotatedElement{{Index: 1, Label: "button", Text: "Sign in", BBox: [4]int{470, 402, 554, 438}}},
	}

	tests := []struct {
		name          string
		args          map[string]any
		annotator     bool
		annotatorErr  error
		wantAnnotated bool
		wantImages    int
		wantNoteSub   string
	}{
		{"default annotated when annotator configured", map[string]any{}, true, nil, true, 0, ""},
		{"default regular without annotator", map[string]any{}, false, nil, false, 1, ""},
		{"explicit annotated replaces image with text", map[string]any{"format": "annotated"}, true, nil, true, 0, ""},
		{"explicit annotated without annotator degrades", map[string]any{"format": "annotated"}, false, nil, false, 1, "annotation unavailable"},
		{"annotator error degrades to regular", map[string]any{"format": "annotated"}, true, errors.New("boom"), false, 1, "annotation failed"},
		{"explicit regular wins over annotator", map[string]any{"format": "regular"}, true, nil, false, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := newFrameTestConfig()
			ctx := context.Background()

			var annotator agentdomain.ImageAnnotator
			if tt.annotator {
				fake := &agentdomainmocks.FakeImageAnnotator{}
				if tt.annotatorErr != nil {
					fake.AnnotateImageReturns(nil, tt.annotatorErr)
				} else {
					fake.AnnotateImageReturns(annotation, nil)
				}
				annotator = fake
			}

			screen := &agentdomainmocks.FakeFrameSource{}
			screen.GetLatestFrameReturns(frameFixture(), nil)
			tool := NewGetLatestFrameTool(cfg, fakeSourceLookup{"screen": screen}, annotator)

			result, err := tool.Execute(ctx, tt.args)
			assert.NoError(t, err)
			assert.True(t, result.Success, "annotation problems must never fail the call: %s", result.Error)

			data := result.Data.(agentdomain.FrameToolResult)
			assert.Equal(t, tt.wantAnnotated, data.Annotated)
			assert.Len(t, result.Images, tt.wantImages)
			if tt.wantNoteSub != "" {
				assert.Contains(t, data.Note, tt.wantNoteSub)
			}
		})
	}
}

func TestGetLatestFrameAnnotatedRender(t *testing.T) {
	cfg := newFrameTestConfig()
	annotation := &agentdomain.ImageAnnotation{
		Summary:  "Login form",
		Elements: []agentdomain.AnnotatedElement{{Index: 1, Label: "button", Text: "Sign in", BBox: [4]int{470, 402, 554, 438}}},
	}
	tool := NewGetLatestFrameTool(cfg, fakeSourceLookup{}, nil)

	screenResult := &agentdomain.ToolExecutionResult{
		ToolName: "GetLatestFrame",
		Success:  true,
		Data:     agentdomain.FrameToolResult{Source: "screen", Annotated: true, Annotation: annotation},
	}
	text := tool.FormatForLLM(screenResult)
	assert.Contains(t, text, "Frame summary: Login form")
	assert.Contains(t, text, `1. button "Sign in" - center (512,420) bbox [470,402,554,438]`)
	assert.Contains(t, text, "MouseClick", "screen source should carry the interaction hint")

	camResult := &agentdomain.ToolExecutionResult{
		ToolName: "GetLatestFrame",
		Success:  true,
		Data:     agentdomain.FrameToolResult{Source: "cam", Annotated: true, Annotation: annotation},
	}
	assert.NotContains(t, tool.FormatForLLM(camResult), "MouseClick", "non-screen sources get no interaction hint")
}

func TestGetLatestFrameValidate(t *testing.T) {
	tool := NewGetLatestFrameTool(newFrameTestConfig(), fakeSourceLookup{}, nil)
	assert.NoError(t, tool.Validate(map[string]any{}))
	assert.NoError(t, tool.Validate(map[string]any{"format": "annotated"}))
	err := tool.Validate(map[string]any{"format": "marked"})
	assert.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "invalid format"))
}

func TestGetLatestFramePerSourcePromptOverride(t *testing.T) {
	cfg := newFrameTestConfig()
	cfg.Vision.Sources = map[string]config.VisionSourceConfig{
		"cam": {Type: "directory", Path: "/frames", Prompt: "look for forklifts"},
	}
	fake := &agentdomainmocks.FakeImageAnnotator{}
	fake.AnnotateImageReturns(&agentdomain.ImageAnnotation{Summary: "ok"}, nil)

	cam := &agentdomainmocks.FakeFrameSource{}
	cam.GetLatestFrameReturns(frameFixture(), nil)
	tool := NewGetLatestFrameTool(cfg, fakeSourceLookup{"cam": cam}, fake)

	result, err := tool.Execute(context.Background(), map[string]any{})
	assert.NoError(t, err)
	assert.True(t, result.Success)

	_, _, opts := fake.AnnotateImageArgsForCall(0)
	assert.Equal(t, "look for forklifts", opts.Prompt)
	assert.Equal(t, 1024, opts.Width)
	assert.Equal(t, 768, opts.Height)
}
