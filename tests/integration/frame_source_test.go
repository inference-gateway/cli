package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	sdk "github.com/inference-gateway/sdk"
	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
)

// framePNG is a minimal 1x1 PNG frame fixture.
var framePNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

// hasImagePart reports whether any message in the request carries an
// image_url content part.
func hasImagePart(body sdk.CreateChatCompletionRequest) bool {
	for _, m := range body.Messages {
		parts, err := m.Content.AsMessageContent1()
		if err != nil {
			continue
		}
		for _, p := range parts {
			if ip, err := p.AsImageContentPart(); err == nil && string(ip.Type) == "image_url" {
				return true
			}
		}
	}
	return false
}

// TestGetLatestFrameAnnotatedWithAnnotator drives the robotics loop
// end-to-end: a directory frame source and a gateway annotator, so annotated
// is the default format. The agent asks for the latest frame, the tool
// side-calls the annotator (an ordinary chat completion through the same
// mock gateway), and the session model receives annotation text - never base64.
func TestGetLatestFrameAnnotatedWithAnnotator(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: annotator-side-call
    match: 'JSON only'
    turns:
      - content: '{"summary":"Warehouse aisle with a forklift","elements":[{"index":1,"label":"forklift","bbox":[60,260,240,420]}]}'
  - name: camera-question
    match: '(?i)what does the camera see'
    turns:
      - tool_calls:
          - { name: GetLatestFrame, args: {} }
      - content: "The camera shows a forklift in a warehouse aisle."
`))
	require.NoError(t, err)

	frameDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(frameDir, "frame-001.png"), framePNG, 0o600))

	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.Vision.Sources = map[string]config.VisionSourceConfig{
			"camera-front": {Type: "directory", Path: frameDir},
		}
		cfg.Vision.Annotator = config.VisionAnnotatorConfig{
			Enabled:   true,
			Model:     testModel,
			MaxTokens: 512,
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := e.runStream(ctx, t, "what does the camera see right now?")
	require.Empty(t, res.errs)
	require.Contains(t, res.content(), "The camera shows a forklift")

	bodies := e.completionBodies()

	var annotatorReqs, imageParts int
	var annotationTextSeen bool
	for _, body := range bodies {
		raw, _ := json.Marshal(body.Messages)
		if strings.Contains(string(raw), "JSON only") {
			annotatorReqs++
			require.True(t, hasImagePart(body), "the annotator side-call must carry the image part")
			continue
		}
		if hasImagePart(body) {
			imageParts++
		}
		if strings.Contains(string(raw), "Warehouse aisle with a forklift") {
			annotationTextSeen = true
		}
	}

	require.Equal(t, 1, annotatorReqs, "expected exactly one annotator side-call")
	require.Zero(t, imageParts, "annotated frames replace the image: no base64 may reach the session model")
	require.True(t, annotationTextSeen, "the annotation text must reach the session model as the tool result")
}

// TestGetLatestFrameRegularWithoutAnnotator checks the default without an
// annotator: the session model receives the raw frame as an image part.
func TestGetLatestFrameRegularWithoutAnnotator(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: image-followup
    match: 'Tool execution returned'
    turns:
      - content: "I can see the frame."
  - name: camera-question
    match: '(?i)what does the camera see'
    turns:
      - tool_calls:
          - { name: GetLatestFrame, args: {} }
`))
	require.NoError(t, err)

	frameDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(frameDir, "frame-001.png"), framePNG, 0o600))

	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.Vision.Sources = map[string]config.VisionSourceConfig{
			"camera-front": {Type: "directory", Path: frameDir},
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := e.runStream(ctx, t, "what does the camera see right now?")
	require.Empty(t, res.errs)
	require.Contains(t, res.content(), "I can see the frame.")

	var sawImage bool
	for _, body := range e.completionBodies() {
		if hasImagePart(body) {
			sawImage = true
		}
	}
	require.True(t, sawImage, "a vision-capable model must receive the raw frame image part")
}
