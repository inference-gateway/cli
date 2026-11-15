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

// localProvider prefixes the models the gateway serves with a built-in engine.
// The gateway takes such a model whole and names no provider for it.
const localProvider = "local"

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

// Render creates the video job for request, polls it until it completes and
// writes the downloaded MP4 content to outPath. An avatar request (audio
// set) is a lip-synced talking clip rendered with text_to_video.avatar_model;
// any other request is rendered with text_to_video.model.
// Errors name the configured model so a gateway without the endpoint (or a
// provider that rejects it) is diagnosable.
func (s *VideoService) Render(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
	model := s.config.TextToVideo.ResolveGatewayModel(request.IsAvatar())
	provider, modelName := config.ParseModel(model)
	if provider == "" || modelName == "" {
		return fmt.Errorf("invalid text_to_video model %q (expected 'provider/model')", model)
	}

	if provider == localProvider {
		provider, modelName = "", model
	}

	videoReq, err := s.buildCreateRequest(request, modelName)
	if err != nil {
		return err
	}

	return s.runVideoJob(ctx, model, provider, videoReq, outPath)
}

// Dub creates the dubbing job for the clip at request.VideoPath through the
// gateway's Videos API, polls it until it completes and writes the downloaded
// MP4 - the clip re-rendered with the generated sound as its new soundtrack -
// to outPath. The model is video_to_sfx.model; the job loop reuses the
// TextToVideo polling code and its timeout and poll_interval knobs.
func (s *VideoService) Dub(ctx context.Context, request agentdomain.VideoDubRequest, outPath string) error {
	model := s.config.VideoToSFX.ResolveGatewayModel()
	provider, modelName := config.ParseModel(model)
	if provider == "" || modelName == "" {
		return fmt.Errorf("invalid video_to_sfx model %q (expected 'provider/model')", model)
	}

	if provider == localProvider {
		provider, modelName = "", model
	}

	dubReq, err := s.buildDubRequest(request, modelName)
	if err != nil {
		return err
	}
	return s.runVideoJob(ctx, model, provider, dubReq, outPath)
}

// buildDubRequest converts the domain request into the SDK's create body: the
// source clip as its video file part, plus the prompt, preserve_speech and
// start-offset passthroughs. The clip must stay within the gateway's default
// body limit, checked before the upload.
func (s *VideoService) buildDubRequest(request agentdomain.VideoDubRequest, modelName string) (sdk.CreateVideoRequest, error) {
	req := sdk.CreateVideoRequest{Model: modelName}
	if prompt := strings.TrimSpace(request.Prompt); prompt != "" {
		req.Prompt = &prompt
	}

	data, err := os.ReadFile(request.VideoPath) // nolint:gosec
	if err != nil {
		return sdk.CreateVideoRequest{}, fmt.Errorf("reading video %q: %w", request.VideoPath, err)
	}
	if len(data) > maxVideoUploadBytes {
		return sdk.CreateVideoRequest{}, fmt.Errorf(
			"video upload (%.1f MiB) exceeds the gateway's default %d MiB request limit; use a shorter clip",
			float64(len(data))/(1<<20), maxVideoUploadBytes/(1<<20),
		)
	}
	var video openapi_types.File
	video.InitFromBytes(data, filepath.Base(request.VideoPath))
	req.Video = &video

	req.PreserveSpeech = request.PreserveSpeech
	req.StartOffsetSeconds = request.StartSeconds
	return req, nil
}

// withJobDeadline bounds a Videos API job, create through download, by
// text_to_video.timeout - the one deadline rendering and dubbing share.
func (s *VideoService) withJobDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := time.Duration(cmp.Or(s.config.TextToVideo.Timeout, defaultVideoTimeoutSeconds)) * time.Second
	return context.WithTimeout(ctx, timeout)
}

// runVideoJob creates the job, polls it until it completes, fails or the
// context ends, and writes the downloaded MP4 content to outPath. Errors name
// the configured model so a gateway without the endpoint (or a provider that
// rejects it) is diagnosable.
func (s *VideoService) runVideoJob(ctx context.Context, model, provider string, req sdk.CreateVideoRequest, outPath string) error {
	ctx, cancel := s.withJobDeadline(ctx)
	defer cancel()

	job, err := s.client.CreateVideo(ctx, sdk.Provider(provider), req)
	if err != nil {
		return fmt.Errorf("video creation with %s failed: %w", model, err)
	}
	if job == nil {
		return fmt.Errorf("video creation with %s returned no job", model)
	}
	return s.finishVideoJob(ctx, model, provider, job.ID, outPath)
}

// finishVideoJob polls the job, downloads its content and writes it to
// outPath - the loop every Videos API job shares. Its timeout comes from
// text_to_video.timeout and its poll interval from text_to_video.poll_interval.
func (s *VideoService) finishVideoJob(ctx context.Context, model, provider, jobID, outPath string) error {
	if err := s.pollVideo(ctx, model, sdk.Provider(provider), jobID); err != nil {
		return err
	}

	content, err := s.client.DownloadVideoContent(ctx, sdk.Provider(provider), jobID)
	if err != nil {
		return fmt.Errorf("downloading video %s with %s failed: %w", jobID, model, err)
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
// driving audio files when a talking clip was requested, and the subject's
// reference images. Size falls back to the configured text_to_video.size
// when the caller did not supply one.
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
	upload := func(path, label string) (*openapi_types.File, error) {
		data, err := os.ReadFile(path) // nolint:gosec
		if err != nil {
			return nil, fmt.Errorf("reading %s %q: %w", label, path, err)
		}
		var file openapi_types.File
		file.InitFromBytes(data, filepath.Base(path))
		uploadBytes += len(data)
		return &file, nil
	}

	var err error
	if request.AvatarPath != "" {
		if req.InputReference, err = upload(request.AvatarPath, "avatar"); err != nil {
			return sdk.CreateVideoRequest{}, err
		}
	}
	if request.AudioPath != "" {
		if req.Audio, err = upload(request.AudioPath, "audio"); err != nil {
			return sdk.CreateVideoRequest{}, err
		}
	}
	if len(request.ReferencePaths) > 0 {
		refs := make([]openapi_types.File, 0, len(request.ReferencePaths))
		for _, path := range request.ReferencePaths {
			file, err := upload(path, "reference image")
			if err != nil {
				return sdk.CreateVideoRequest{}, err
			}
			refs = append(refs, *file)
		}
		req.ReferenceImages = &refs
	}

	if uploadBytes > maxVideoUploadBytes {
		return sdk.CreateVideoRequest{}, fmt.Errorf(
			"avatar images plus audio upload (%.1f MiB) exceeds the gateway's 10 MiB request limit; use a shorter clip, an MP3 audio or fewer avatar images",
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
