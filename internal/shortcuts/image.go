package shortcuts

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// ImageShortcut generates an image via the gateway's images endpoint and saves
// it to a local PNG file, printing the saved path in the conversation.
type ImageShortcut struct {
	client       sdk.Client
	modelService domain.ModelService
	httpClient   *http.Client
}

// NewImageShortcut creates a new ImageShortcut.
func NewImageShortcut(client sdk.Client, modelService domain.ModelService) *ImageShortcut {
	return &ImageShortcut{
		client:       client,
		modelService: modelService,
		httpClient:   http.DefaultClient,
	}
}

func (s *ImageShortcut) GetName() string { return "image" }
func (s *ImageShortcut) GetDescription() string {
	return "Generate an image and save it to a local file"
}
func (s *ImageShortcut) GetUsage() string              { return "/image <prompt>" }
func (s *ImageShortcut) CanExecute(args []string) bool { return len(args) > 0 }

func (s *ImageShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	model := s.modelService.GetCurrentModel()
	if model == "" {
		return ShortcutResult{Output: "No model selected - use /model to select one first", Success: false}, nil
	}

	slash := strings.Index(model, "/")
	if slash == -1 {
		return ShortcutResult{Output: fmt.Sprintf("Invalid model %q (expected 'provider/model')", model), Success: false}, nil
	}
	provider := sdk.Provider(model[:slash])
	modelName := model[slash+1:]

	resp, err := s.client.CreateImage(ctx, provider, sdk.CreateImageRequest{
		Model:  &modelName,
		Prompt: strings.Join(args, " "),
	})
	if err != nil {
		return ShortcutResult{Output: fmt.Sprintf("Image generation failed: %v", err), Success: false}, nil
	}

	if len(resp.Data) == 0 {
		return ShortcutResult{Output: "Image generation returned no images", Success: false}, nil
	}

	data, err := imageBytes(ctx, s.httpClient, resp.Data[0])
	if err != nil {
		return ShortcutResult{Output: fmt.Sprintf("Failed to read generated image: %v", err), Success: false}, nil
	}

	path := fmt.Sprintf("image-%d.png", time.Now().Unix())
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ShortcutResult{Output: fmt.Sprintf("Failed to save image: %v", err), Success: false}, nil
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("Image saved to %s", path),
		Success: true,
	}, nil
}

// imageBytes resolves the generated image payload: base64 data first, falling
// back to downloading the URL.
func imageBytes(ctx context.Context, client *http.Client, img sdk.Image) ([]byte, error) {
	if img.B64Json != nil && *img.B64Json != "" {
		return base64.StdEncoding.DecodeString(*img.B64Json)
	}
	if img.URL != nil && *img.URL != "" {
		return downloadImage(ctx, client, *img.URL)
	}
	return nil, fmt.Errorf("image response contained neither base64 data nor a URL")
}

// downloadImage fetches an image from a URL.
func downloadImage(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
