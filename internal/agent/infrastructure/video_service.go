package infrastructure

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// maxVideoUploadBytes is the gateway's default request body limit
// (SERVER_MAX_REQUEST_BODY_SIZE): the avatar portrait and driving audio go
// in one multipart body, so their combined size is checked before sending.
const maxVideoUploadBytes = 10 << 20

// Defaults for the video job loop (seconds).
const (
	defaultVideoTimeoutSeconds      = 900
	defaultVideoPollIntervalSeconds = 5
)

// VideoService renders video clips through the gateway's Videos API
// (POST /v1/videos, GET /v1/videos/{id}, GET /v1/videos/{id}/content), so
// requests show up in gateway logs and traces. It implements
// agentdomain.VideoService.
type VideoService struct {
	config *config.Config
	client sdk.Client
}

// NewVideoService creates a new gateway-backed video service.
func NewVideoService(cfg *config.Config, client sdk.Client) *VideoService {
	return &VideoService{
		config: cfg,
		client: client,
	}
}

// Render creates the video job for request using the configured
// text_to_video.model ("provider/model"), polls it until it completes and
// writes the downloaded MP4 content to outPath. With AvatarPath and
// AudioPath set the job is a lip-synced talking clip; text-only otherwise.
// Errors name the configured model so a gateway without the endpoint (or a
// provider that rejects it) is diagnosable.
func (s *VideoService) Render(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
	model := s.config.TextToVideo.ResolveGatewayModel()
	provider, modelName, ok := strings.Cut(model, "/")
	if !ok || provider == "" || modelName == "" {
		return fmt.Errorf("invalid text_to_video.model %q (expected 'provider/model')", model)
	}

	videoReq, err := s.buildCreateRequest(request, modelName)
	if err != nil {
		return err
	}

	timeout := time.Duration(cmp.Or(s.config.TextToVideo.Timeout, defaultVideoTimeoutSeconds)) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	job, err := s.client.CreateVideo(ctx, sdk.Provider(provider), videoReq)
	if err != nil {
		return fmt.Errorf("video creation with %s failed: %w", model, err)
	}
	if job == nil {
		return fmt.Errorf("video creation with %s returned no job", model)
	}

	if err := s.pollVideo(ctx, model, sdk.Provider(provider), job.ID); err != nil {
		return err
	}

	content, err := s.client.DownloadVideoContent(ctx, sdk.Provider(provider), job.ID)
	if err != nil {
		return fmt.Errorf("downloading video %s with %s failed: %w", job.ID, model, err)
	}
	if len(content) == 0 {
		return fmt.Errorf("video generation with %s returned no content", model)
	}

	if err := os.WriteFile(outPath, content, 0o644); err != nil { // nolint:gosec
		return fmt.Errorf("writing video to %q: %w", outPath, err)
	}
	return nil
}

// buildCreateRequest converts the domain request into the SDK's create body:
// prompt and the seconds/size passthroughs, plus the avatar portrait and
// driving audio files when a talking clip was requested. Size falls back to
// the configured text_to_video.size when the caller did not supply one.
func (s *VideoService) buildCreateRequest(request agentdomain.VideoRequest, modelName string) (sdk.CreateVideoRequest, error) {
	req := sdk.CreateVideoRequest{Model: modelName}
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		req.Prompt = &prompt
	}
	if seconds := strings.TrimSpace(request.Seconds); seconds != "" {
		req.Seconds = &seconds
	}
	if size := strings.TrimSpace(cmp.Or(request.Size, s.config.TextToVideo.Size)); size != "" {
		req.Size = &size
	}

	var uploadBytes int
	if request.AvatarPath != "" {
		avatar, err := os.ReadFile(request.AvatarPath) // nolint:gosec
		if err != nil {
			return sdk.CreateVideoRequest{}, fmt.Errorf("reading avatar %q: %w", request.AvatarPath, err)
		}
		var file openapi_types.File
		file.InitFromBytes(avatar, filepath.Base(request.AvatarPath))
		req.InputReference = &file
		uploadBytes += len(avatar)
	}
	if request.AudioPath != "" {
		audio, err := os.ReadFile(request.AudioPath) // nolint:gosec
		if err != nil {
			return sdk.CreateVideoRequest{}, fmt.Errorf("reading audio %q: %w", request.AudioPath, err)
		}
		var file openapi_types.File
		file.InitFromBytes(audio, filepath.Base(request.AudioPath))
		req.Audio = &file
		uploadBytes += len(audio)
	}

	if uploadBytes > maxVideoUploadBytes {
		return sdk.CreateVideoRequest{}, fmt.Errorf(
			"avatar plus audio upload (%.1f MiB) exceeds the gateway's 10 MiB request limit; use a shorter clip or an MP3 audio",
			float64(uploadBytes)/(1<<20),
		)
	}
	return req, nil
}

// pollVideo retrieves the job every poll_interval until it completes, fails
// or the context ends; the interval is the configured
// text_to_video.poll_interval (default 5s).
func (s *VideoService) pollVideo(ctx context.Context, model string, provider sdk.Provider, videoID string) error {
	interval := time.Duration(cmp.Or(s.config.TextToVideo.PollInterval, defaultVideoPollIntervalSeconds)) * time.Second
	for {
		job, err := s.client.RetrieveVideo(ctx, provider, videoID)
		if err != nil {
			return fmt.Errorf("retrieving video job %s with %s failed: %w", videoID, model, err)
		}
		switch job.Status {
		case sdk.VideoJobStatusCompleted:
			return nil
		case sdk.VideoJobStatusFailed:
			return fmt.Errorf("video generation with %s failed: %s", model, videoJobErrorMessage(job))
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("video generation with %s did not complete in time: %w", model, ctx.Err())
		case <-time.After(interval):
		}
	}
}

// videoJobErrorMessage extracts the provider's error.message from a failed job.
func videoJobErrorMessage(job *sdk.VideoJob) string {
	if job.Error != nil && job.Error.Message != nil && strings.TrimSpace(*job.Error.Message) != "" {
		return *job.Error.Message
	}
	return "unknown error"
}
