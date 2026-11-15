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

func newTestVideoSFXTool(t *testing.T, enabled bool, videoSfx agentdomain.VideoSoundEffectService) *VideoToSFXTool {
	t.Helper()
	t.Chdir(t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.VideoToSFX.Enabled = enabled
	cfg.VideoToSFX.OutputDir = t.TempDir()
	return NewVideoToSFXTool(cfg, videoSfx)
}

// writeVideoFixture writes a clip into the working directory the tool resolves
// input names from.
func writeVideoFixture(t *testing.T, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(name, []byte("fake-video-bytes"), 0o600))
}

// scoreAsWAV stubs a scoring call whose answer is WAV: the track lands under
// the resolved name with a .wav extension, as the gateway service saves it.
func scoreAsWAV(t *testing.T) func(context.Context, agentdomain.VideoSoundEffectRequest, string) (string, error) {
	t.Helper()
	return func(_ context.Context, _ agentdomain.VideoSoundEffectRequest, outPath string) (string, error) {
		saved := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".wav"
		require.NoError(t, os.WriteFile(saved, []byte("scored"), 0o600))
		return saved, nil
	}
}

func TestVideoToSFXTool_IsEnabled(t *testing.T) {
	t.Run("enabled with a video-SFX service", func(t *testing.T) {
		tool := newTestVideoSFXTool(t, true, &agentdomainmocks.FakeVideoSoundEffectService{})
		assert.True(t, tool.IsEnabled())
	})
	t.Run("disabled in config", func(t *testing.T) {
		tool := newTestVideoSFXTool(t, false, &agentdomainmocks.FakeVideoSoundEffectService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled without a video-SFX service", func(t *testing.T) {
		tool := newTestVideoSFXTool(t, true, nil)
		assert.False(t, tool.IsEnabled())
	})
}

func TestVideoToSFXTool_Definition(t *testing.T) {
	tool := newTestVideoSFXTool(t, true, &agentdomainmocks.FakeVideoSoundEffectService{})
	def := tool.Definition()

	assert.Equal(t, ToolVideoToSFX, def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "video")

	params := *def.Function.Parameters
	assert.Equal(t, []string{"video"}, params["required"])
	properties := params["properties"].(map[string]any)
	assert.Contains(t, properties, "video")
	assert.Contains(t, properties, "prompt")
	assert.Contains(t, properties, "seconds")
	assert.Contains(t, properties, "start_seconds")
	assert.Contains(t, properties, "track")
	assert.Contains(t, properties, "preserve_speech")
	assert.Contains(t, properties, "output_path")

	var tracks []string
	for _, track := range agentdomain.SoundEffectTracks {
		tracks = append(tracks, string(track))
	}
	assert.Equal(t, tracks, properties["track"].(map[string]any)["enum"], "the manifest offers exactly the domain's tracks")
}

func TestVideoToSFXTool_Validate(t *testing.T) {
	tool := newTestVideoSFXTool(t, true, &agentdomainmocks.FakeVideoSoundEffectService{})

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"video only", map[string]any{"video": "clip.mp4"}, ""},
		{"all knobs", map[string]any{"video": "clip.mp4", "prompt": "soft taps", "seconds": 4.0, "start_seconds": 2.0, "track": "speech", "preserve_speech": true}, ""},
		{"mix track", map[string]any{"video": "clip.mp4", "track": "mix"}, ""},
		{"named output", map[string]any{"video": "clip.mp4", "output_path": "claps.mp3"}, ""},
		{"missing video", map[string]any{}, "video is required"},
		{"empty video", map[string]any{"video": "  "}, "video is required"},
		{"non-string video", map[string]any{"video": 42}, "video is required"},
		{"unsupported extension", map[string]any{"video": "clip.avi"}, "must be one of .mov, .mp4, .m4v, .webm"},
		{"no extension", map[string]any{"video": "clip"}, "must be one of .mov, .mp4, .m4v, .webm"},
		{"non-number seconds", map[string]any{"video": "clip.mp4", "seconds": "4"}, "seconds must be a number"},
		{"seconds below one", map[string]any{"video": "clip.mp4", "seconds": 0.5}, "seconds must be at least 1"},
		{"non-number start", map[string]any{"video": "clip.mp4", "start_seconds": "2"}, "start_seconds must be a number"},
		{"negative start", map[string]any{"video": "clip.mp4", "start_seconds": -1.0}, "start_seconds must not be negative"},
		{"unknown track", map[string]any{"video": "clip.mp4", "track": "vox"}, "track must be one of [mix sfx speech]"},
		{"non-string track", map[string]any{"video": "clip.mp4", "track": 1}, "track must be a string"},
		{"non-bool preserve_speech", map[string]any{"video": "clip.mp4", "preserve_speech": "yes"}, "preserve_speech must be a boolean"},
		{"absolute output path", map[string]any{"video": "clip.mp4", "output_path": "/tmp/claps.mp3"}, "invalid output_path"},
		{"output path with directory", map[string]any{"video": "clip.mp4", "output_path": "sub/claps.mp3"}, "invalid output_path"},
		{"output path with ..", map[string]any{"video": "clip.mp4", "output_path": "../claps.mp3"}, "invalid output_path"},
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

func TestVideoToSFXTool_Execute(t *testing.T) {
	t.Run("reports the saved track, clip and window", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreStub = func(ctx context.Context, request agentdomain.VideoSoundEffectRequest, outPath string) (string, error) {
			require.NoError(t, os.WriteFile(outPath, []byte("scored-bytes"), 0o644))
			return outPath, nil
		}
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":           "clip.mp4",
			"prompt":          "soft keyboard taps",
			"seconds":         4.0,
			"start_seconds":   1.0,
			"track":           "sfx",
			"preserve_speech": false,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.Success)

		data, ok := res.Data.(map[string]any)
		require.True(t, ok)
		path, _ := data["path"].(string)
		assert.Contains(t, path, "sfx-")
		assert.Equal(t, "clip.mp4", data["video"])
		assert.Equal(t, "soft keyboard taps", data["prompt"])
		assert.Equal(t, "sfx", data["track"])
		assert.FileExists(t, path)

		require.Equal(t, 1, svc.ScoreCallCount())
		_, request, _ := svc.ScoreArgsForCall(0)
		assert.True(t, strings.HasSuffix(request.VideoPath, "clip.mp4"), "the clip resolves from the working directory")
		assert.Equal(t, "soft keyboard taps", request.Prompt)
		assert.Equal(t, agentdomain.SoundEffectTrackSFX, request.Track)
		require.NotNil(t, request.Seconds)
		assert.InDelta(t, 4, *request.Seconds, 0.01)
		require.NotNil(t, request.StartSeconds)
		assert.InDelta(t, 1, *request.StartSeconds, 0.01)
		require.NotNil(t, request.PreserveSpeech)
		assert.False(t, *request.PreserveSpeech)
	})

	t.Run("an unknown clip fails before the service is called", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		tool := newTestVideoSFXTool(t, true, svc)

		res, err := tool.Execute(context.Background(), map[string]any{"video": "gone.mov"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "video \"gone.mov\" not found")
		assert.Equal(t, 0, svc.ScoreCallCount())
	})

	t.Run("omitted knobs are nil", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreStub = func(ctx context.Context, request agentdomain.VideoSoundEffectRequest, outPath string) (string, error) {
			require.NoError(t, os.WriteFile(outPath, []byte("scored"), 0o644))
			return outPath, nil
		}
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")

		_, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
		require.NoError(t, err)

		_, request, _ := svc.ScoreArgsForCall(0)
		assert.Nil(t, request.Seconds)
		assert.Nil(t, request.StartSeconds)
		assert.Nil(t, request.PreserveSpeech)
		assert.Empty(t, request.Track)
	})

	t.Run("service failure reports the error and leaves no partial file", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreReturns("", fmt.Errorf("video scoring is not supported"))
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")

		res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "video scoring is not supported")

		entries, readErr := os.ReadDir(tool.config.VideoToSFX.OutputDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "a failed call must not leave a partial file")
	})

	t.Run("failure with a named output leaves the caller's target untouched", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreReturns("", fmt.Errorf("boom"))
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":       "clip.mp4",
			"output_path": "named.mp3",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.NoFileExists(t, filepath.Join(tool.config.VideoToSFX.OutputDir, "named.mp3"))
	})

	t.Run("invalid output_path fails without calling the service", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		tool := newTestVideoSFXTool(t, true, svc)

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":       "clip.mp4",
			"output_path": "/etc/passwd",
		})
		require.ErrorContains(t, err, "invalid output_path")
		assert.Nil(t, res)
		assert.Equal(t, 0, svc.ScoreCallCount())
	})
}

func TestVideoToSFXTool_ExecuteSavedUnderAnotherExtension(t *testing.T) {
	t.Run("a track saved under another extension replaces the allocated placeholder", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreStub = scoreAsWAV(t)
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")

		res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
		require.NoError(t, err)
		require.True(t, res.Success, res.Error)

		entries, readErr := os.ReadDir(tool.config.VideoToSFX.OutputDir)
		require.NoError(t, readErr)
		require.Len(t, entries, 1, "the empty placeholder must not survive next to the track")
		assert.Equal(t, ".wav", filepath.Ext(entries[0].Name()))
	})

	t.Run("a track saved under another extension keeps the file already at the named output", func(t *testing.T) {
		svc := &agentdomainmocks.FakeVideoSoundEffectService{}
		svc.ScoreStub = scoreAsWAV(t)
		tool := newTestVideoSFXTool(t, true, svc)
		writeVideoFixture(t, "clip.mp4")
		earlier := filepath.Join(tool.config.VideoToSFX.OutputDir, "take.mp3")
		require.NoError(t, os.WriteFile(earlier, []byte("earlier-track"), 0o600))

		res, err := tool.Execute(context.Background(), map[string]any{
			"video":       "clip.mp4",
			"output_path": "take.mp3",
		})
		require.NoError(t, err)
		require.True(t, res.Success, res.Error)

		assert.FileExists(t, earlier, "a file the call did not create must not be deleted")
		assert.FileExists(t, filepath.Join(tool.config.VideoToSFX.OutputDir, "take.wav"))
	})
}

func TestVideoToSFXTool_Formatting(t *testing.T) {
	svc := &agentdomainmocks.FakeVideoSoundEffectService{}
	svc.ScoreStub = func(ctx context.Context, request agentdomain.VideoSoundEffectRequest, outPath string) (string, error) {
		require.NoError(t, os.WriteFile(outPath, []byte("scored"), 0o644))
		return outPath, nil
	}
	tool := newTestVideoSFXTool(t, true, svc)
	writeVideoFixture(t, "clip.mp4")

	res, err := tool.Execute(context.Background(), map[string]any{"video": "clip.mp4"})
	require.NoError(t, err)

	assert.Contains(t, tool.FormatPreview(res), "Sound effect saved to")
	assert.Contains(t, tool.FormatForLLM(res), "Sound effect saved to")
	assert.False(t, strings.Contains(tool.FormatPreview(res), "\n"))
}

func TestVideoToSFXTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.VideoToSFX.Enabled = false
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		assert.NotContains(t, registry.ListAvailableTools(), ToolVideoToSFX)
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, ToolVideoToSFX, def.Function.Name)
		}
		_, err := registry.GetTool(ToolVideoToSFX)
		assert.Error(t, err)
	})

	t.Run("enabled: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.VideoToSFX.Enabled = true
		cfg.VideoToSFX.OutputDir = t.TempDir()
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, &agentdomainmocks.FakeVideoSoundEffectService{}, nil, nil, nil, nil)

		assert.Contains(t, registry.ListAvailableTools(), ToolVideoToSFX)

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == ToolVideoToSFX {
				found = true
			}
		}
		assert.True(t, found, "VideoToSFX definition should be in the tools payload")
	})
}
