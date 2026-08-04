package integration

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	require "github.com/stretchr/testify/require"

	mockgateway "github.com/inference-gateway/tokenless/gateway"

	config "github.com/inference-gateway/cli/config"
)

// onePixelPNG is a base64-encoded 1x1 transparent PNG - the smallest payload
// a real image decoder accepts (same constant as the mock gateway).
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk" +
	"YPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

// TestImageDecodeFromURL drives the full loop: the chat model requests the
// ImageDecode tool with an http(s) URL, the tool fetches the image from the
// URL (instead of reading from a local file), and the annotation result is
// returned to the model.
func TestImageDecodeFromURL(t *testing.T) {
	rawPNG, err := base64.StdEncoding.DecodeString(onePixelPNG)
	require.NoError(t, err)

	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(rawPNG)
	}))
	t.Cleanup(imgSrv.Close)

	imageURL := imgSrv.URL + "/test.png"

	defs, err := mockgateway.Load([]byte(fmt.Sprintf(`
fallback:
  content: '{"summary":"A test image.","elements":[{"index":1,"label":"pixel","text":"single pixel","bbox":[0,0,1,1]}]}'
scenarios:
  - name: image-decode-url
    match: '(?i)decode this image'
    turns:
      - tool_calls:
          - { name: ImageDecode, args: { image: "%s" } }
      - content: "Image decoded successfully."
`, imageURL)))
	require.NoError(t, err)

	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.Vision.Annotator.Enabled = true
		cfg.Vision.Annotator.Model = "openai/gpt-4o"
		cfg.Vision.Annotator.MaxTokens = 256
		cfg.Vision.Annotator.Timeout = 10
	})

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := e.runStream(ctx, t, "decode this image URL")
	require.Empty(t, res.errs)
	require.Contains(t, res.content(), "Image decoded successfully.")

	bodies := e.completionBodies()
	require.Len(t, bodies, 3, "initial + annotation + follow-up")

	firstAssistant := lastAssistant(t, bodies[2])
	require.NotNil(t, firstAssistant.ToolCalls)
	calls := *firstAssistant.ToolCalls
	require.Len(t, calls, 1)
	require.Equal(t, "ImageDecode", calls[0].Function.Name)
	require.Contains(t, calls[0].Function.Arguments, imageURL,
		"tool call arguments must contain the image URL")

	tools := toolMessages(bodies[2])
	require.Len(t, tools, 1, "tool result must be sent back to the gateway")
	content, err := tools[0].Content.AsMessageContent0()
	require.NoError(t, err)
	require.Contains(t, content, "A test image.",
		"tool result must contain the annotation summary")

	reqs := e.gateway.Requests()
	require.Len(t, reqs, 3, "three chat completion requests: initial + annotation + follow-up")
	require.Equal(t, "image-decode-url", reqs[0].Scenario, "first request must match the image-decode-url scenario")
}

// TestImageDecodeFromFileIsUnchanged verifies that local file paths still work
// (the URL branch does not break the existing file-based path).
func TestImageDecodeFromFileIsUnchanged(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: '{"summary":"A test image.","elements":[{"index":1,"label":"pixel","text":"single pixel","bbox":[0,0,1,1]}]}'
scenarios:
  - name: image-decode-file
    match: '(?i)decode the local image'
    turns:
      - tool_calls:
          - { name: ImageDecode, args: { image: "test-image.png" } }
      - content: "Local image decoded."
`))
	require.NoError(t, err)

	e := newEnvWithScenarios(t, defs, func(cfg *config.Config) {
		cfg.Vision.Annotator.Enabled = true
		cfg.Vision.Annotator.Model = "openai/gpt-4o"
		cfg.Vision.Annotator.MaxTokens = 256
		cfg.Vision.Annotator.Timeout = 10
	})

	rawPNG, err := base64.StdEncoding.DecodeString(onePixelPNG)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("test-image.png", rawPNG, 0o644))

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := e.runStream(ctx, t, "decode the local image")
	require.Empty(t, res.errs)
	require.Contains(t, res.content(), "Local image decoded.")

	bodies := e.completionBodies()
	require.Len(t, bodies, 3, "initial + annotation + follow-up")

	firstAssistant := lastAssistant(t, bodies[2])
	require.NotNil(t, firstAssistant.ToolCalls)
	calls := *firstAssistant.ToolCalls
	require.Len(t, calls, 1)
	require.Equal(t, "ImageDecode", calls[0].Function.Name)
	require.Contains(t, calls[0].Function.Arguments, "test-image.png",
		"tool call arguments must contain the local file path")
}
