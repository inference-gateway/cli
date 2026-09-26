package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	avatars "github.com/inference-gateway/cli/internal/avatars"
)

// minimalPNG is the smallest valid PNG: 1x1 transparent pixel header.
func minimalPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
		0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
		0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
		0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func newTestVideoTool(t *testing.T, enabled bool, video agentdomain.VideoService) *TextToVideoTool {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToVideo.Enabled = enabled
	cfg.TextToVideo.OutputDir = t.TempDir()
	cfg.TextToSpeech.OutputDir = t.TempDir()
	return NewTextToVideoTool(cfg, video)
}

func TestTextToVideoTool_IsEnabled(t *testing.T) {
	t.Run("enabled with a video service", func(t *testing.T) {
		tool := newTestVideoTool(t, true, &agentdomainmocks.FakeVideoService{})
		assert.True(t, tool.IsEnabled())
	})
	t.Run("disabled in config", func(t *testing.T) {
		tool := newTestVideoTool(t, false, &agentdomainmocks.FakeVideoService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled without a video service", func(t *testing.T) {
		tool := newTestVideoTool(t, true, nil)
		assert.False(t, tool.IsEnabled())
	})
}

func TestTextToVideoTool_Definition(t *testing.T) {
	tool := newTestVideoTool(t, true, &agentdomainmocks.FakeVideoService{})
	def := tool.Definition()

	assert.Equal(t, "TextToVideo", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "video")

	properties := (*def.Function.Parameters)["properties"].(map[string]any)
	for _, name := range []string{"prompt", "seconds", "size", "avatar", "audio", "output_path"} {
		assert.Contains(t, properties, name)
	}
	assert.Contains(t, *def.Function.Description, "720x1280")
}

func TestTextToVideoTool_Validate(t *testing.T) {
	tool := newTestVideoTool(t, true, &agentdomainmocks.FakeVideoService{})

	avatarPath := filepath.Join(t.TempDir(), "avatar.png")
	require.NoError(t, os.WriteFile(avatarPath, minimalPNG(), 0o600))
	workDir := filepath.Dir(avatarPath)
	t.Chdir(workDir)

	require.NoError(t, os.WriteFile(filepath.Join(workDir, "line.mp3"), []byte("mp3"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tool.config.TextToSpeech.OutputDir, "line.wav"), minimalWAV(), 0o600))
	writeAvatar(t, "presenter", "01-front.png", "02-left.jpg")

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"text only", map[string]any{"prompt": "a neon city flyover"}, ""},
		{"avatar plus wav audio", map[string]any{"prompt": "framing", "avatar": "avatar.png", "audio": "line.wav"}, ""},
		{"avatar plus mp3 audio, no prompt", map[string]any{"avatar": "avatar.png", "audio": "line.mp3"}, ""},
		{"audio without avatar", map[string]any{"prompt": "p", "audio": "line.wav"}, "audio requires avatar"},
		{"neither prompt nor audio", map[string]any{}, "prompt is required unless audio is provided"},
		{"audio of the wrong kind", map[string]any{"prompt": "p", "avatar": "avatar.png", "audio": "line.flac"}, "must be a .wav or .mp3 file"},
		{"library avatar plus audio", map[string]any{"avatar": "presenter", "audio": "line.wav"}, ""},
		{"library avatar as first frame", map[string]any{"prompt": "p", "avatar": "presenter"}, ""},
		{"unknown avatar lists the library", map[string]any{"prompt": "p", "avatar": "ghost"}, "available avatars: presenter"},
		{"unsupported image is not a library avatar", map[string]any{"prompt": "p", "avatar": "avatar.gif"}, "not found"},
		{"library avatar with ..", map[string]any{"prompt": "p", "avatar": ".."}, "invalid avatar name"},
		{"avatar not found", map[string]any{"prompt": "p", "avatar": "missing.png"}, "not found"},
		{"audio not found", map[string]any{"prompt": "p", "avatar": "avatar.png", "audio": "missing.wav"}, "not found"},
		{"absolute audio", map[string]any{"prompt": "p", "avatar": "avatar.png", "audio": filepath.Join(workDir, "line.wav")}, "invalid audio path"},
		{"avatar with directory", map[string]any{"prompt": "p", "avatar": "sub/avatar.png"}, "invalid avatar path"},
		{"avatar with ..", map[string]any{"prompt": "p", "avatar": "../avatar.png"}, "invalid avatar path"},
		{"bare output path", map[string]any{"prompt": "p", "output_path": "clip.mp4"}, ""},
		{"absolute output path", map[string]any{"prompt": "p", "output_path": "/tmp/clip.mp4"}, "invalid output_path"},
		{"output path with directory", map[string]any{"prompt": "p", "output_path": "sub/clip.mp4"}, "invalid output_path"},
		{"output path with ..", map[string]any{"prompt": "p", "output_path": "../clip.mp4"}, "invalid output_path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTextToVideoTool_Execute(t *testing.T) { // nolint:funlen
	t.Run("text-only render reports the result shape", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":  "a neon city flyover",
			"seconds": "4",
			"size":    "1280x720",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.Success)

		data, ok := res.Data.(map[string]any)
		require.True(t, ok)
		path, _ := data["path"].(string)
		assert.Contains(t, path, "video-")
		assert.True(t, strings.HasSuffix(path, ".mp4"))
		assert.Equal(t, config.TextToVideoGatewayDefaultModel, data["model"])
		assert.Equal(t, "4", data["seconds"])
		assert.Equal(t, "1280x720", data["size"])
		assert.Equal(t, "", data["avatar"])
		assert.Equal(t, "", data["audio"])

		require.Equal(t, 1, video.RenderCallCount())
		_, req, gotOut := video.RenderArgsForCall(0)
		assert.Equal(t, "a neon city flyover", req.Prompt)
		assert.Equal(t, "4", req.Seconds)
		assert.Equal(t, "1280x720", req.Size)
		assert.Empty(t, req.AvatarPath)
		assert.Empty(t, req.AudioPath)
		assert.Equal(t, filepath.Dir(gotOut), filepath.Dir(path))
	})

	t.Run("avatar plus wav render resolves the lookup and passes both paths", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "face.png"), minimalPNG(), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(tool.config.TextToSpeech.OutputDir, "line.wav"), minimalWAV(), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{
			"avatar": "face.png",
			"audio":  "line.wav",
		})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)

		data, _ := res.Data.(map[string]any)
		assert.Equal(t, "face.png", data["avatar"])
		assert.Equal(t, "line.wav", data["audio"])
		assert.Equal(t, config.TextToVideoGatewayDefaultAvatarModel, data["model"])

		_, req, _ := video.RenderArgsForCall(0)
		assert.True(t, strings.HasSuffix(req.AvatarPath, "face.png"))
		assert.True(t, strings.HasSuffix(req.AudioPath, "line.wav"))
		assert.Equal(t, "", req.Prompt)
	})

	t.Run("avatar plus mp3 render", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "face.png"), minimalPNG(), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "line.mp3"), []byte("mp3"), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt": "close up on the presenter",
			"avatar": "face.png",
			"audio":  "line.mp3",
		})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)

		_, req, _ := video.RenderArgsForCall(0)
		assert.Equal(t, "close up on the presenter", req.Prompt)
		assert.True(t, strings.HasSuffix(req.AudioPath, "line.mp3"))
	})

	t.Run("library avatar renders from its primary image", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)
		dir := writeAvatar(t, "presenter", "02-left.jpg", "01-front.png")
		require.NoError(t, os.WriteFile(filepath.Join(tool.config.TextToSpeech.OutputDir, "line.wav"), minimalWAV(), 0o600))

		res, err := tool.Execute(context.Background(), map[string]any{"avatar": "presenter", "audio": "line.wav"})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)

		_, req, _ := video.RenderArgsForCall(0)
		assert.Equal(t, filepath.Join(dir, "01-front.png"), req.AvatarPath)
		assert.Empty(t, req.ReferencePaths, "lip-sync models take a single portrait")
		assert.True(t, req.IsAvatar())
	})

	t.Run("library avatar without audio sends every image as a reference", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)
		dir := writeAvatar(t, "presenter", "02-left.png", "01-front.jpg", "03-right.png")

		res, err := tool.Execute(context.Background(), map[string]any{"avatar": "presenter", "prompt": "the presenter waves"})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)
		assert.Equal(t, config.TextToVideoGatewayDefaultModel, res.Data.(map[string]any)["model"])

		_, req, _ := video.RenderArgsForCall(0)
		assert.Equal(t, []string{
			filepath.Join(dir, "01-front.jpg"),
			filepath.Join(dir, "02-left.png"),
			filepath.Join(dir, "03-right.png"),
		}, req.ReferencePaths)
		assert.Empty(t, req.AvatarPath, "the gateway rejects reference images together with input_reference")
	})

	t.Run("image file without audio stays the first frame", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestVideoTool(t, true, video)
		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "face.png"), minimalPNG(), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{"avatar": "face.png", "prompt": "a slow zoom"})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)

		_, req, _ := video.RenderArgsForCall(0)
		assert.Equal(t, filepath.Join(workDir, "face.png"), req.AvatarPath)
		assert.Empty(t, req.ReferencePaths)
	})

	t.Run("provider failure reports the error and leaves no partial file", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderReturns(fmt.Errorf("video generation with elevenlabs/creatify-aurora failed: content policy violation"))
		tool := newTestVideoTool(t, true, video)

		res, err := tool.Execute(context.Background(), map[string]any{"prompt": "a riser"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "content policy violation")

		entries, readErr := os.ReadDir(tool.config.TextToVideo.OutputDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "a failed render must not leave a partial file")
	})

	t.Run("failure with a named output leaves the caller's target untouched", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.RenderReturns(fmt.Errorf("boom"))
		tool := newTestVideoTool(t, true, video)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "a riser",
			"output_path": "named.mp4",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.NoFileExists(t, filepath.Join(tool.config.TextToVideo.OutputDir, "named.mp4"))
	})

	t.Run("invalid output_path fails without calling the service", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		tool := newTestVideoTool(t, true, video)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "a riser",
			"output_path": "/etc/passwd",
		})
		require.ErrorContains(t, err, "invalid output_path")
		assert.Nil(t, res)
		assert.Equal(t, 0, video.RenderCallCount())
	})
}

func TestTextToVideoTool_Formatting(t *testing.T) {
	video := &agentdomainmocks.FakeVideoService{}
	video.RenderStub = func(ctx context.Context, request agentdomain.VideoRequest, outPath string) error {
		return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
	}
	tool := newTestVideoTool(t, true, video)

	res, err := tool.Execute(context.Background(), map[string]any{"prompt": "a neon city flyover"})
	require.NoError(t, err)

	assert.Contains(t, tool.FormatPreview(res), "Video saved to")
	assert.Contains(t, tool.FormatForLLM(res), "Video saved to")
	assert.False(t, strings.Contains(tool.FormatPreview(res), "\n"))
}

func TestTextToVideoTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToVideo.Enabled = false
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		assert.NotContains(t, registry.ListAvailableTools(), "TextToVideo")
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, "TextToVideo", def.Function.Name)
		}
		_, err := registry.GetTool("TextToVideo")
		assert.Error(t, err)
	})

	t.Run("enabled: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToVideo.Enabled = true
		cfg.TextToVideo.OutputDir = t.TempDir()
		registry := NewRegistry(cfg, nil, nil, nil, nil, &agentdomainmocks.FakeVideoService{}, nil, nil, nil, nil)

		assert.Contains(t, registry.ListAvailableTools(), "TextToVideo")

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == "TextToVideo" {
				found = true
			}
		}
		assert.True(t, found, "TextToVideo definition should be in the tools payload")
	})
}

// writeAvatar creates ~/.infer/avatars/<name>/ under the test HOME holding
// the given images and returns the avatar folder.
func writeAvatar(t *testing.T, name string, images ...string) string {
	t.Helper()
	dir := filepath.Join(avatars.Dir(), name)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	for _, image := range images {
		require.NoError(t, os.WriteFile(filepath.Join(dir, image), minimalPNG(), 0o600))
	}
	return dir
}
