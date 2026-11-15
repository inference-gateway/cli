package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// GatewayEndpoint is where raw gateway HTTP requests go and how they are
// authenticated. The SDK client covers the generated JSON API; this carries
// the endpoint it cannot express, the multipart SFX video form.
type GatewayEndpoint struct {
	Client *http.Client
	URL    string
	APIKey string
}

// The gateway polls a video-to-sfx job inside a fixed 150 s budget and the
// clip upload precedes it, so the client deadline adds upload slack on top.
const (
	sfxVideoJobBudget   = 150 * time.Second
	sfxVideoUploadSlack = 90 * time.Second
)

// VideoSFXService scores a video clip through the gateway's SFX API
// (POST /v1/audio/sfx, multipart/form-data), so requests show up in gateway
// logs and traces. It implements agentdomain.VideoSoundEffectService.
type VideoSFXService struct {
	config   *config.Config
	endpoint GatewayEndpoint
}

// NewVideoSFXService creates a new gateway-backed video-scoring service.
func NewVideoSFXService(cfg *config.Config, endpoint GatewayEndpoint) *VideoSFXService {
	return &VideoSFXService{
		config:   cfg,
		endpoint: endpoint,
	}
}

// Score uploads the clip as the video part of /v1/audio/sfx, writes the raw
// audio the gateway answers with to outPath and returns the saved path, whose
// extension matches the response content type. Errors carry the gateway's own
// message so a clip without a wanted track or a provider that refuses a knob
// is diagnosable.
func (s *VideoSFXService) Score(ctx context.Context, request agentdomain.VideoSoundEffectRequest, outPath string) (string, error) {
	model := s.config.VideoToSFX.ResolveGatewayModel()
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelName == "" {
		return outPath, fmt.Errorf("invalid video_to_sfx.model %q (expected 'provider/model')", model)
	}

	clip, err := os.ReadFile(request.VideoPath) // nolint:gosec
	if err != nil {
		return outPath, fmt.Errorf("reading video %q: %w", request.VideoPath, err)
	}

	body, formType, err := s.buildForm(modelName, request, clip)
	if err != nil {
		return outPath, fmt.Errorf("building the video form: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, sfxVideoJobBudget+sfxVideoUploadSlack)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.scoreURL(provider), body)
	if err != nil {
		return outPath, fmt.Errorf("sfx scoring with %s: %w", model, err)
	}
	req.Header.Set("Content-Type", formType)
	if s.endpoint.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.endpoint.APIKey)
	}

	resp, err := s.endpoint.Client.Do(req)
	if err != nil {
		return outPath, fmt.Errorf("sfx scoring with %s failed: %w", model, err)
	}
	defer func() { _ = resp.Body.Close() }()

	audio, err := readScoredAudio(resp, model)
	if err != nil {
		return outPath, err
	}
	return writeScoredAudio(outPath, resp.Header.Get("Content-Type"), audio)
}

// scoreURL is the endpoint the multipart form posts to: /audio/sfx plus the
// provider query parameter every gateway request carries.
func (s *VideoSFXService) scoreURL(provider string) string {
	return fmt.Sprintf("%s/audio/sfx?provider=%s", s.endpoint.URL, url.QueryEscape(provider))
}

// buildForm encodes the SFX video form: the unprefixed model, the optional
// knobs as fields and the clip as the video file part.
func (s *VideoSFXService) buildForm(model string, request agentdomain.VideoSoundEffectRequest, clip []byte) (*bytes.Buffer, string, error) {
	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)

	var formErr error
	setField := func(name, value string) {
		if formErr == nil && value != "" {
			formErr = writer.WriteField(name, value)
		}
	}
	setField("model", model)
	setField("prompt", strings.TrimSpace(request.Prompt))
	if request.Seconds != nil {
		setField("duration_seconds", strconv.FormatFloat(float64(*request.Seconds), 'f', -1, 32))
	}
	if request.StartSeconds != nil {
		setField("start_offset_seconds", strconv.FormatFloat(float64(*request.StartSeconds), 'f', -1, 32))
	}
	if request.PreserveSpeech != nil {
		setField("preserve_speech", strconv.FormatBool(*request.PreserveSpeech))
	}
	setField("output", request.Track)
	if formErr != nil {
		return nil, "", formErr
	}

	part, err := writer.CreateFormFile("video", filepath.Base(request.VideoPath))
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(clip); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return form, writer.FormDataContentType(), nil
}

// readScoredAudio returns the raw audio the gateway answered with, or an
// error carrying the gateway's own message; a 413 names the request-body
// limit so the operator knows which gateway knob to raise.
func readScoredAudio(resp *http.Response, model string) ([]byte, error) {
	if resp.StatusCode >= http.StatusMultipleChoices {
		err := gatewayErrorMessage(resp.Body)
		if resp.StatusCode == http.StatusRequestEntityTooLarge {
			return nil, fmt.Errorf("sfx scoring with %s: the clip exceeds the gateway's SERVER_MAX_REQUEST_BODY_SIZE; send a shorter clip or raise the gateway's limit: %w", model, err)
		}
		return nil, fmt.Errorf("sfx scoring with %s failed: %w", model, err)
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading scored audio from the gateway: %w", err)
	}
	if len(audio) == 0 {
		return nil, fmt.Errorf("sfx scoring with %s returned no audio", model)
	}
	return audio, nil
}

// gatewayErrorMessage builds the error the gateway's {"error": ...} envelope
// carries - a plain string or a nested message - falling back to the raw body.
func gatewayErrorMessage(body io.Reader) error {
	raw, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading the gateway's error: %w", err)
	}
	var envelope struct {
		Error *json.RawMessage `json:"error"`
	}
	if json.Unmarshal(raw, &envelope) == nil && envelope.Error != nil {
		var message string
		if json.Unmarshal(*envelope.Error, &message) == nil && strings.TrimSpace(message) != "" {
			return fmt.Errorf("%s", message)
		}
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(*envelope.Error, &nested) == nil && strings.TrimSpace(nested.Message) != "" {
			return fmt.Errorf("%s", nested.Message)
		}
	}
	if len(raw) > 0 {
		return fmt.Errorf("%s", strings.TrimSpace(string(raw)))
	}
	return fmt.Errorf("no error message")
}

// scoredFileExtension maps the audio Content-Type the gateway answered with
// to the file extension the track carries. An unrecognized type keeps the
// resolved name's extension.
func scoredFileExtension(contentType, fallback string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(contentType)
	}
	switch mediaType {
	case "audio/mpeg", "audio/mp3":
		return ".mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return ".wav"
	case "audio/ogg":
		return ".ogg"
	case "audio/flac":
		return ".flac"
	case "audio/aac":
		return ".aac"
	case "audio/mp4", "audio/x-m4a", "audio/m4a":
		return ".m4a"
	default:
		return filepath.Ext(fallback)
	}
}

// writeScoredAudio writes the answered audio to outPath and returns the saved
// path, fixing the extension to the response's content type; a resolved
// timestamped placeholder under another extension does not survive.
func writeScoredAudio(outPath, contentType string, audio []byte) (string, error) {
	saved := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + scoredFileExtension(contentType, outPath)
	if err := os.WriteFile(saved, audio, 0o644); err != nil { // nolint:gosec
		return outPath, fmt.Errorf("writing audio to %q: %w", saved, err)
	}
	if saved != outPath {
		_ = os.Remove(outPath)
	}
	return saved, nil
}
