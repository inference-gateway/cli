package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// videosRoute is the gateway's Videos API route under the base URL.
const videosRoute = "/videos"

// Fields of the multipart dubbing form the gateway reads.
const (
	dubFormModel          = "model"
	dubFormPrompt         = "prompt"
	dubFormPreserveSpeech = "preserve_speech"
	dubFormVideo          = "video"
)

// bytesPerMiB converts an upload size to the unit the gateway's limit is quoted in.
const bytesPerMiB = 1 << 20

// maxDubErrorBodyBytes caps how much of a failed response is read into the error.
const maxDubErrorBodyBytes = 4096

// VideoDubGateway is the dubbing create's gateway: the SDK client covers
// every other Videos API call, but its CreateVideo does not forward the
// dubbing fields (the video part and preserve_speech) yet, so the dub create
// posts its own multipart form to the same endpoint and returns the job.
type VideoDubGateway interface {
	CreateDubbing(ctx context.Context, provider, model string, request agentdomain.VideoDubRequest) (*sdk.VideoJob, error)
}

// DubGateway posts the dubbing create against the gateway's Videos API with
// the SDK client's shape: bearer auth, ?provider= routing when set, the job
// JSON on success and the gateway's {"error": "..."} bodies on failure.
type DubGateway struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewDubGateway creates a dubbing create gateway over the gateway's base URL.
func NewDubGateway(baseURL, apiKey string, client *http.Client) *DubGateway {
	return &DubGateway{baseURL: baseURL, apiKey: apiKey, client: client}
}

// CreateDubbing posts the multipart create - the model and the optional
// prompt and preserve_speech as fields, the clip as its video file part.
// shortcut: the SDK's CreateVideo drops the dubbing fields; encode them here
// until the SDK forwards them, then switch back to sdk.Client.CreateVideo
func (g *DubGateway) CreateDubbing(ctx context.Context, provider, model string, request agentdomain.VideoDubRequest) (*sdk.VideoJob, error) {
	clip, err := os.Open(request.VideoPath) // nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading video %q: %w", request.VideoPath, err)
	}
	defer func() { _ = clip.Close() }()

	info, err := clip.Stat()
	if err != nil {
		return nil, fmt.Errorf("reading video %q: %w", request.VideoPath, err)
	}
	if info.Size() > maxVideoUploadBytes {
		return nil, fmt.Errorf(
			"video upload (%.1f MiB) exceeds the gateway's default %d MiB request limit; use a shorter clip",
			float64(info.Size())/bytesPerMiB, maxVideoUploadBytes/bytesPerMiB,
		)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fields := map[string]string{dubFormModel: model}
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		fields[dubFormPrompt] = prompt
	}
	if request.PreserveSpeech != nil {
		fields[dubFormPreserveSpeech] = strconv.FormatBool(*request.PreserveSpeech)
	}
	for name, value := range fields {
		if err := mw.WriteField(name, value); err != nil {
			return nil, fmt.Errorf("writing the %s field: %w", name, err)
		}
	}
	part, err := mw.CreateFormFile(dubFormVideo, filepath.Base(request.VideoPath))
	if err != nil {
		return nil, fmt.Errorf("creating the video part: %w", err)
	}
	if _, err := io.Copy(part, clip); err != nil {
		return nil, fmt.Errorf("writing the video part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("closing the multipart body: %w", err)
	}

	path := g.baseURL + videosRoute
	if provider != "" {
		path += "?" + url.Values{providerQueryParam: {provider}}.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("building the dub request: %w", err)
	}
	req.Header.Set(headerContentType, mw.FormDataContentType())
	if g.apiKey != "" {
		req.Header.Set(headerAuthorization, bearerScheme+g.apiKey)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("posting the dub request: %w", err)
	}
	defer resp.Body.Close() // nolint:errcheck

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, dubAPIError(resp)
	}

	var job sdk.VideoJob
	if err := json.NewDecoder(resp.Body).Decode(&job); err != nil {
		return nil, fmt.Errorf("decoding the dubbing job: %w", err)
	}
	return &job, nil
}

// dubAPIError mirrors the SDK's apiError: prefer the gateway's
// {"error": "..."} body, fall back to the raw body.
func dubAPIError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxDubErrorBodyBytes)) // nolint:gosec
	var errorResp struct {
		Error *string `json:"error"`
	}
	if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != nil && *errorResp.Error != "" {
		return fmt.Errorf("API error: %s (status code: %d)", *errorResp.Error, resp.StatusCode)
	}
	if len(body) > 0 {
		return fmt.Errorf("video request failed with status: %d, response body: %s", resp.StatusCode, body)
	}
	return fmt.Errorf("video request failed with status: %d", resp.StatusCode)
}
