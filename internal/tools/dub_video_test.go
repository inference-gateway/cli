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
)

func newTestDubTool(t *testing.T, enabled, dub bool, video agentdomain.VideoService) *DubVideoTool {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToVideo.OutputDir = t.TempDir()
	cfg.VideoToSFX = config.VideoToSFXConfig{Enabled: enabled, Dub: dub}
	return NewDubVideoTool(cfg, video)
}

func TestDubVideoTool_IsEnabled(t *testing.T) {
	t.Run("enabled and dub set with a video service", func(t *testing.T) {
		tool := newTestDubTool(t, true, true, &agentdomainmocks.FakeVideoService{})
		assert.True(t, tool.IsEnabled())
	})
	t.Run("dub alone is not enough", func(t *testing.T) {
		tool := newTestDubTool(t, false, true, &agentdomainmocks.FakeVideoService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled alone is not enough", func(t *testing.T) {
		tool := newTestDubTool(t, true, false, &agentdomainmocks.FakeVideoService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled without a video service", func(t *testing.T) {
		tool := newTestDubTool(t, true, true, nil)
		assert.False(t, tool.IsEnabled())
	})
}

func TestDubVideoTool_ManifestRequireApproval(t *testing.T) {
	yes := true
	tests := []struct {
		name       string
		configured *bool
		want       bool
	}{
		{"unset keeps the manifest default of no approval", nil, false},
		{"video_to_sfx.require_approval covers the dub too", &yes, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := newTestDubTool(t, true, true, &agentdomainmocks.FakeVideoService{})
			tool.config.VideoToSFX.RequireApproval = tt.configured

			assert.Equal(t, tt.want, tool.Manifest().RequiresApproval(false))
		})
	}
}

func TestDubVideoTool_Definition(t *testing.T) {
	tool := newTestDubTool(t, true, true, &agentdomainmocks.FakeVideoService{})
	def := tool.Definition()

	assert.Equal(t, ToolDubVideo, def.Function.Name)
	require.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "only the generated mix", "the description must state Mirelo's mix-only limit")
	assert.Contains(t, *def.Function.Description, "no track parameter")
	assert.NotContains(t, *def.Function.Description, "track:")

	properties := (*def.Function.Parameters)["properties"].(map[string]any)
	for _, name := range []string{"video", "prompt", "preserve_speech", "output_path"} {
		assert.Contains(t, properties, name)
	}
	assert.Len(t, properties, 4)
	assert.Equal(t, []string{"video"}, (*def.Function.Parameters)["required"].([]string))
}

func TestDubVideoTool_Validate(t *testing.T) {
	tool := newTestDubTool(t, true, true, &agentdomainmocks.FakeVideoService{})

	workDir := t.TempDir()
	t.Chdir(workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tool.config.TextToVideo.OutputDir, "render.m4v"), []byte("mp4"), 0o600))

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"clip in the working directory", map[string]any{"video": "clip.mp4"}, ""},
		{"clip name with surrounding spaces", map[string]any{"video": " clip.mp4 "}, ""},
		{"clip in the TextToVideo output directory", map[string]any{"video": "render.m4v"}, ""},
		{"mov clip", map[string]any{"video": "clip.mov", "prompt": "p"}, "not found"},
		{"missing video", map[string]any{}, "video is required"},
		{"blank video", map[string]any{"video": " "}, "video is required"},
		{"wrong extension", map[string]any{"video": "line.wav"}, "must be one of .mov, .mp4, .m4v, .webm"},
		{"absolute clip", map[string]any{"video": "/tmp/clip.mp4"}, "invalid video path"},
		{"clip with directory", map[string]any{"video": "sub/clip.mp4"}, "invalid video path"},
		{"clip with ..", map[string]any{"video": "../clip.mp4"}, "invalid video path"},
		{"non-boolean preserve_speech", map[string]any{"video": "clip.mp4", "preserve_speech": "yes"}, "preserve_speech must be a boolean"},
		{"bare output path", map[string]any{"video": "clip.mp4", "output_path": "dubbed.mp4"}, ""},
		{"absolute output path", map[string]any{"video": "clip.mp4", "output_path": "/tmp/dubbed.mp4"}, "invalid output_path"},
		{"output path with directory", map[string]any{"video": "clip.mp4", "output_path": "sub/dubbed.mp4"}, "invalid output_path"},
		{"output path with ..", map[string]any{"video": "clip.mp4", "output_path": "../dubbed.mp4"}, "invalid output_path"},
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

func TestDubVideoTool_Execute(t *testing.T) {
	t.Run("dub reports the result shape and passes the request", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.DubStub = func(ctx context.Context, request agentdomain.VideoDubRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestDubTool(t, true, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":           "clip.mp4",
			"prompt":          "a busy cafe hubbub",
			"preserve_speech": true,
			"output_path":     "dubbed.mp4",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.Success)

		data, ok := res.Data.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, filepath.Join(tool.config.TextToVideo.OutputDir, "dubbed.mp4"), data["path"])
		assert.Equal(t, config.VideoToSFXGatewayDefaultModel, data["model"])
		assert.Equal(t, "clip.mp4", data["video"])
		assert.Equal(t, true, data["preserve_speech"])

		require.Equal(t, 1, video.DubCallCount())
		_, req, gotOut := video.DubArgsForCall(0)
		assert.Equal(t, "a busy cafe hubbub", req.Prompt)
		require.NotNil(t, req.PreserveSpeech)
		assert.True(t, *req.PreserveSpeech)
		assert.True(t, strings.HasSuffix(req.VideoPath, "clip.mp4"))
		assert.Equal(t, data["path"], gotOut)
	})

	t.Run("preserve_speech omitted keeps the pointer nil", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.DubStub = func(ctx context.Context, request agentdomain.VideoDubRequest, outPath string) error {
			return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
		}
		tool := newTestDubTool(t, true, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)
		assert.Equal(t, false, res.Data.(map[string]any)["preserve_speech"])

		_, req, _ := video.DubArgsForCall(0)
		assert.Nil(t, req.PreserveSpeech)
		assert.Equal(t, "", req.Prompt)
		assert.True(t, strings.HasSuffix(res.Data.(map[string]any)["path"].(string), ".mp4"))
	})

	t.Run("provider failure reports the error and removes the timestamped file", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.DubReturns(fmt.Errorf("dubbing failed: bad clip"))
		tool := newTestDubTool(t, true, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4", "prompt": "wind"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "bad clip")

		entries, readErr := os.ReadDir(tool.config.TextToVideo.OutputDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "a failed dub must not leave a partial file")
	})

	t.Run("failure with a named output leaves the caller's target untouched", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		video.DubReturns(fmt.Errorf("boom"))
		tool := newTestDubTool(t, true, true, video)

		workDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
		t.Chdir(workDir)

		res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4", "output_path": "named.mp4"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.NoFileExists(t, filepath.Join(tool.config.TextToVideo.OutputDir, "named.mp4"))
	})

	t.Run("invalid output_path fails without calling the service", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		tool := newTestDubTool(t, true, true, video)

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":       clipIn(t),
			"output_path": "/etc/passwd",
		})
		require.ErrorContains(t, err, "invalid output_path")
		assert.Nil(t, res)
		assert.Equal(t, 0, video.DubCallCount())
	})

	t.Run("missing clip fails without calling the service", func(t *testing.T) {
		video := &agentdomainmocks.FakeVideoService{}
		tool := newTestDubTool(t, true, true, video)
		t.Chdir(filepath.Dir(tool.config.TextToVideo.OutputDir))

		res, err := tool.Execute(context.Background(), map[string]any{"video": "ghost.mp4"})
		require.ErrorContains(t, err, "ghost.mp4")
		assert.Nil(t, res)
		assert.Equal(t, 0, video.DubCallCount())
	})
}

func TestDubVideoTool_Formatting(t *testing.T) {
	video := &agentdomainmocks.FakeVideoService{}
	video.DubStub = func(ctx context.Context, request agentdomain.VideoDubRequest, outPath string) error {
		return os.WriteFile(outPath, []byte("mp4"), 0o644) // nolint:gosec
	}
	tool := newTestDubTool(t, true, true, video)

	workDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "clip.mp4"), []byte("mp4"), 0o600))
	t.Chdir(workDir)

	res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
	require.NoError(t, err)

	assert.Contains(t, tool.FormatPreview(res), "Video saved to")
	assert.Contains(t, tool.FormatForLLM(res), "Video saved to")
	assert.False(t, strings.Contains(tool.FormatPreview(res), "\n"))

	failed := tool.FormatForLLM(&agentdomain.ToolExecutionResult{Success: false, Error: "boom"})
	assert.Contains(t, failed, "Video dubbing failed")
	assert.Contains(t, failed, "boom")
}

func TestDubVideoTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		registry := NewRegistry(cfg, RegistryDeps{})

		assert.NotContains(t, registry.ListAvailableTools(), ToolDubVideo)
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, ToolDubVideo, def.Function.Name)
		}
		_, err := registry.GetTool(ToolDubVideo)
		assert.Error(t, err)
	})

	t.Run("enabled with dub: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.VideoToSFX = config.VideoToSFXConfig{Enabled: true, Dub: true}
		registry := NewRegistry(cfg, RegistryDeps{Video: &agentdomainmocks.FakeVideoService{}})

		assert.Contains(t, registry.ListAvailableTools(), ToolDubVideo)

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == ToolDubVideo {
				found = true
			}
		}
		assert.True(t, found, "DubVideo definition should be in the tools payload")
	})

	t.Run("enabled without dub: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.VideoToSFX = config.VideoToSFXConfig{Enabled: true}
		registry := NewRegistry(cfg, RegistryDeps{Video: &agentdomainmocks.FakeVideoService{}})

		assert.NotContains(t, registry.ListAvailableTools(), ToolDubVideo)
	})
}

// clipIn writes a small clip into a fresh working directory and returns its
// bare name, chdir'd so the tool looks it up there.
func clipIn(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clip.mp4"), []byte("mp4"), 0o600))
	return "clip.mp4"
}
