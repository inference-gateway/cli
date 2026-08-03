package integration

import (
	"context"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	mockgateway "github.com/inference-gateway/tokenless/mockgateway"
)

// TestImageGenerationToolHitsImagesEndpoint drives the full loop: the chat
// model requests the ImageGeneration tool, the tool sends a plain one-off
// request with the configured image model to /v1/images/generations, and the
// returned payload is saved as a local PNG.
func TestImageGenerationToolHitsImagesEndpoint(t *testing.T) {
	defs, err := mockgateway.Load([]byte(`
fallback:
  content: "Done."
scenarios:
  - name: image-meme
    match: '(?i)generate a meme of a cat'
    turns:
      - tool_calls:
          - { name: ImageGeneration, args: { prompt: "a funny cat meme" } }
      - content: "Meme generated."
`))
	require.NoError(t, err)

	e := newEnvWithScenarios(t, defs)
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	res := e.runStream(ctx, t, "generate a meme of a cat")
	require.Empty(t, res.errs)
	require.Contains(t, res.content(), "Meme generated.")

	var imageReqs []mockgateway.Recorded
	for _, rec := range e.gateway.Requests() {
		if rec.ImagesBody != nil {
			imageReqs = append(imageReqs, rec)
		}
	}
	require.Len(t, imageReqs, 1)
	rec := imageReqs[0]
	require.Equal(t, "/v1/images/generations", rec.Endpoint)
	require.Equal(t, "openai", rec.Provider)
	require.Equal(t, "gpt-image-2", rec.Model)
	require.Equal(t, "a funny cat meme", rec.ImagesBody.Prompt)
	require.NotNil(t, rec.ImagesBody.Quality)
	require.Equal(t, "low", string(*rec.ImagesBody.Quality))
	require.NotNil(t, rec.ImagesBody.Size)
	require.Equal(t, "1024x1024", string(*rec.ImagesBody.Size))

	saved, err := filepath.Glob(filepath.Join(".infer", "tmp", "image-*.png"))
	require.NoError(t, err)
	require.Len(t, saved, 1, "expected exactly one saved PNG in the sandbox cwd")
}
