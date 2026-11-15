package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
// authenticated. The SDK client covers the generated JSON API. This carries
// the endpoint it cannot express, the multipart SFX video form.
type GatewayEndpoint struct {
	Client *http.Client
	URL    string
	APIKey string
}

// Header names and the bearer scheme the raw gateway requests carry.
const (
	headerContentType   = "Content-Type"
	headerAuthorization = "Authorization"
	bearerScheme        = "Bearer "
)

// The gateway polls a video-to-sfx job inside a fixed 150 s budget and the
// clip upload precedes it, so the client deadline adds upload slack on top.
const (
	sfxVideoJobBudget   = 150 * time.Second
	sfxVideoUploadSlack = 90 * time.Second
)

// The gateway's SFX route under the endpoint URL and the query parameter
// that names the provider.
const (
	sfxRoute           = "/audio/sfx"
	providerQueryParam = "provider"
)

// Fields of the multipart SFX video form the gateway reads.
const (
	sfxFormModel          = "model"
	sfxFormPrompt         = "prompt"
	sfxFormDuration       = "duration_seconds"
	sfxFormStartOffset    = "start_offset_seconds"
	sfxFormPreserveSpeech = "preserve_speech"
	sfxFormOutput         = "output"
	sfxFormVideo          = "video"
)

// scoredAudioFileMode lets the user's other programs open the saved track.
const scoredAudioFileMode os.FileMode = 0o644

// audioExtensions maps the audio media types the gateway answers with to the
// file extension the saved track carries.
var audioExtensions = map[string]string{
	"audio/mpeg":  ".mp3",
	"audio/mp3":   ".mp3",
	"audio/wav":   ".wav",
	"audio/x-wav": ".wav",
	"audio/wave":  ".wav",
	"audio/ogg":   ".ogg",
	"audio/flac":  ".flac",
	"audio/aac":   ".aac",
	"audio/mp4":   ".m4a",
	"audio/x-m4a": ".m4a",
	"audio/m4a":   ".m4a",
}

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
	provider, modelName := config.ParseModel(model)
	if provider == "" || modelName == "" {
		return outPath, fmt.Errorf("invalid video_to_sfx.model %q (expected 'provider/model')", model)
	}

	clip, err := os.Open(request.VideoPath) // nolint:gosec
	if err != nil {
		return outPath, fmt.Errorf("reading video %q: %w", request.VideoPath, err)
	}
	defer func() { _ = clip.Close() }()

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
	req.Header.Set(headerContentType, formType)
	if s.endpoint.APIKey != "" {
		req.Header.Set(headerAuthorization, bearerScheme+s.endpoint.APIKey)
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
	return writeScoredAudio(outPath, resp.Header.Get(headerContentType), audio)
}

// scoreURL is the endpoint the multipart form posts to: /audio/sfx plus the
// provider query parameter every gateway request carries.
func (s *VideoSFXService) scoreURL(provider string) string {
	return s.endpoint.URL + sfxRoute + "?" + url.Values{providerQueryParam: {provider}}.Encode()
}

// buildForm encodes the SFX video form: the unprefixed model, the optional
// knobs as fields and the clip as the video file part.
func (s *VideoSFXService) buildForm(model string, request agentdomain.VideoSoundEffectRequest, clip io.Reader) (*bytes.Buffer, string, error) {
	form := &bytes.Buffer{}
	writer := multipart.NewWriter(form)

	var formErr error
	setField := func(name, value string) {
		if formErr == nil && value != "" {
			formErr = writer.WriteField(name, value)
		}
	}
	setField(sfxFormModel, model)
	setField(sfxFormPrompt, strings.TrimSpace(request.Prompt))
	if request.Seconds != nil {
		setField(sfxFormDuration, formatSeconds(*request.Seconds))
	}
	if request.StartSeconds != nil {
		setField(sfxFormStartOffset, formatSeconds(*request.StartSeconds))
	}
	if request.PreserveSpeech != nil {
		setField(sfxFormPreserveSpeech, strconv.FormatBool(*request.PreserveSpeech))
	}
	setField(sfxFormOutput, string(request.Track))
	if formErr != nil {
		return nil, "", formErr
	}

	part, err := writer.CreateFormFile(sfxFormVideo, filepath.Base(request.VideoPath))
	if err != nil {
		return nil, "", err
	}
	if _, err := io.Copy(part, clip); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return form, writer.FormDataContentType(), nil
}

// formatSeconds renders a duration field in the shortest decimal form that
// reads back as the same float32, so 1.5 goes out as "1.5".
func formatSeconds(seconds float32) string {
	const shortestPrecision, float32Bits = -1, 32
	return strconv.FormatFloat(float64(seconds), 'f', shortestPrecision, float32Bits)
}

// readScoredAudio returns the raw audio the gateway answered with, or an
// error carrying the gateway's own message. A 413 names the request-body
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
			return errors.New(message)
		}
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(*envelope.Error, &nested) == nil && strings.TrimSpace(nested.Message) != "" {
			return errors.New(nested.Message)
		}
	}
	if len(raw) > 0 {
		return errors.New(strings.TrimSpace(string(raw)))
	}
	return errors.New("no error message")
}

// scoredFileExtension maps the audio Content-Type the gateway answered with
// to the file extension the track carries. An unrecognized type keeps the
// resolved name's extension.
func scoredFileExtension(contentType, fallback string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(contentType)
	}
	if extension, known := audioExtensions[mediaType]; known {
		return extension
	}
	return filepath.Ext(fallback)
}

// writeScoredAudio writes the answered audio under outPath's name and returns
// the saved path, whose extension follows the response's content type. A file
// already at outPath is the caller's to keep or remove.
func writeScoredAudio(outPath, contentType string, audio []byte) (string, error) {
	saved := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + scoredFileExtension(contentType, outPath)
	if err := os.WriteFile(saved, audio, scoredAudioFileMode); err != nil { // nolint:gosec
		return outPath, fmt.Errorf("writing audio to %q: %w", saved, err)
	}
	return saved, nil
}
