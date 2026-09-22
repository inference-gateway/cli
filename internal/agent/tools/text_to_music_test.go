package tools

import (
	"bytes"
	"context"
	"encoding/binary"
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

// fakeWav returns a minimal valid WAV (1s of silence at 24 kHz 16-bit mono),
// the smallest payload audio.WAVDurationSeconds accepts.
func fakeWav(t *testing.T) []byte {
	t.Helper()
	data := make([]byte, 48000)
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+len(data)))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(24000))
	_ = binary.Write(&buf, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}

func newTestMusicTool(t *testing.T, enabled bool, music agentdomain.MusicService) *TextToMusicTool {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToMusic.Enabled = enabled
	cfg.TextToMusic.OutputDir = t.TempDir()
	return NewTextToMusicTool(cfg, music)
}

func TestTextToMusicTool_IsEnabled(t *testing.T) {
	t.Run("enabled with a music service", func(t *testing.T) {
		tool := newTestMusicTool(t, true, &agentdomainmocks.FakeMusicService{})
		assert.True(t, tool.IsEnabled())
	})
	t.Run("disabled in config", func(t *testing.T) {
		tool := newTestMusicTool(t, false, &agentdomainmocks.FakeMusicService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled without a music service", func(t *testing.T) {
		tool := newTestMusicTool(t, true, nil)
		assert.False(t, tool.IsEnabled())
	})
}

func TestTextToMusicTool_Definition(t *testing.T) {
	tool := newTestMusicTool(t, true, &agentdomainmocks.FakeMusicService{})
	def := tool.Definition()

	assert.Equal(t, "TextToMusic", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "music")

	params := *def.Function.Parameters
	assert.Equal(t, []string{"prompt"}, params["required"])
	properties := params["properties"].(map[string]any)
	assert.Contains(t, properties, "prompt")
	assert.Contains(t, properties, "seconds")
	assert.Contains(t, properties, "instrumental")
	assert.Contains(t, properties, "output_path")
}

func TestTextToMusicTool_Validate(t *testing.T) {
	tool := newTestMusicTool(t, true, &agentdomainmocks.FakeMusicService{})

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"prompt only", map[string]any{"prompt": "calm piano"}, ""},
		{"with seconds", map[string]any{"prompt": "calm piano", "seconds": 30.0}, ""},
		{"with instrumental", map[string]any{"prompt": "calm piano", "instrumental": true}, ""},
		{"bare output path", map[string]any{"prompt": "calm piano", "output_path": "loop.wav"}, ""},
		{"missing prompt", map[string]any{}, "prompt is required"},
		{"empty prompt", map[string]any{"prompt": "  "}, "prompt is required"},
		{"non-string prompt", map[string]any{"prompt": 42}, "prompt is required"},
		{"non-number seconds", map[string]any{"prompt": "p", "seconds": "30"}, "seconds must be a number"},
		{"zero seconds", map[string]any{"prompt": "p", "seconds": 0.0}, "seconds must be a positive number"},
		{"negative seconds", map[string]any{"prompt": "p", "seconds": -5.0}, "seconds must be a positive number"},
		{"non-bool instrumental", map[string]any{"prompt": "p", "instrumental": "yes"}, "instrumental must be a boolean"},
		{"absolute output path", map[string]any{"prompt": "p", "output_path": "/tmp/loop.wav"}, "invalid output_path"},
		{"output path with directory", map[string]any{"prompt": "p", "output_path": "sub/loop.wav"}, "invalid output_path"},
		{"output path with ..", map[string]any{"prompt": "p", "output_path": "../loop.wav"}, "invalid output_path"},
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

func TestTextToMusicTool_Execute(t *testing.T) {
	t.Run("reports path, prompt and duration", func(t *testing.T) {
		music := &agentdomainmocks.FakeMusicService{}
		music.ComposeStub = func(ctx context.Context, prompt, outPath string, seconds *float32, instrumental *bool) error {
			return os.WriteFile(outPath, fakeWav(t), 0o644)
		}
		tool := newTestMusicTool(t, true, music)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":       "calm piano loop",
			"seconds":      30.0,
			"instrumental": true,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.Success)

		data, ok := res.Data.(map[string]any)
		require.True(t, ok)
		path, _ := data["path"].(string)
		assert.Contains(t, path, "music-")
		assert.Equal(t, "calm piano loop", data["prompt"])
		assert.InDelta(t, 1.0, data["duration_seconds"], 0.01)

		require.Equal(t, 1, music.ComposeCallCount())
		_, gotPrompt, gotOut, gotSeconds, gotInstrumental := music.ComposeArgsForCall(0)
		assert.Equal(t, "calm piano loop", gotPrompt)
		assert.Equal(t, filepath.Dir(gotOut), filepath.Dir(path))
		require.NotNil(t, gotSeconds)
		assert.InDelta(t, 30.0, *gotSeconds, 0.01)
		require.NotNil(t, gotInstrumental)
		assert.True(t, *gotInstrumental)
	})

	t.Run("omitted knobs are nil", func(t *testing.T) {
		music := &agentdomainmocks.FakeMusicService{}
		music.ComposeStub = func(ctx context.Context, prompt, outPath string, seconds *float32, instrumental *bool) error {
			return os.WriteFile(outPath, fakeWav(t), 0o644)
		}
		tool := newTestMusicTool(t, true, music)

		_, err := tool.Execute(context.Background(), map[string]any{"prompt": "lo-fi beat"})
		require.NoError(t, err)

		_, _, _, gotSeconds, gotInstrumental := music.ComposeArgsForCall(0)
		assert.Nil(t, gotSeconds)
		assert.Nil(t, gotInstrumental)
	})

	t.Run("provider failure reports the error and leaves no partial file", func(t *testing.T) {
		music := &agentdomainmocks.FakeMusicService{}
		music.ComposeReturns(fmt.Errorf("provider does not support music"))
		tool := newTestMusicTool(t, true, music)

		res, err := tool.Execute(context.Background(), map[string]any{"prompt": "calm piano"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "does not support music")

		entries, readErr := os.ReadDir(tool.config.TextToMusic.OutputDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "a failed call must not leave a partial file")
	})

	t.Run("failure with a named output leaves the caller's target untouched", func(t *testing.T) {
		music := &agentdomainmocks.FakeMusicService{}
		music.ComposeReturns(fmt.Errorf("boom"))
		tool := newTestMusicTool(t, true, music)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "calm piano",
			"output_path": "named.wav",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.NoFileExists(t, filepath.Join(tool.config.TextToMusic.OutputDir, "named.wav"))
	})

	t.Run("invalid output_path fails without calling the service", func(t *testing.T) {
		music := &agentdomainmocks.FakeMusicService{}
		tool := newTestMusicTool(t, true, music)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "calm piano",
			"output_path": "/etc/passwd",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "invalid output_path")
		assert.Equal(t, 0, music.ComposeCallCount())
	})
}

func TestTextToMusicTool_Formatting(t *testing.T) {
	music := &agentdomainmocks.FakeMusicService{}
	music.ComposeStub = func(ctx context.Context, prompt, outPath string, seconds *float32, instrumental *bool) error {
		return os.WriteFile(outPath, fakeWav(t), 0o644)
	}
	tool := newTestMusicTool(t, true, music)

	res, err := tool.Execute(context.Background(), map[string]any{"prompt": "calm piano"})
	require.NoError(t, err)

	assert.Contains(t, tool.FormatPreview(res), "Music saved to")
	assert.Contains(t, tool.FormatForLLM(res), "Music saved to")
	assert.Contains(t, tool.FormatForLLM(res), "1.0s of audio")
	assert.False(t, strings.Contains(tool.FormatPreview(res), "\n"))
}

func TestTextToMusicTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToMusic.Enabled = false
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil)

		assert.NotContains(t, registry.ListAvailableTools(), "TextToMusic")
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, "TextToMusic", def.Function.Name)
		}
		_, err := registry.GetTool("TextToMusic")
		assert.Error(t, err)
	})

	t.Run("enabled: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToMusic.Enabled = true
		cfg.TextToMusic.OutputDir = t.TempDir()
		registry := NewRegistry(cfg, nil, nil, &agentdomainmocks.FakeMusicService{}, nil, nil, nil, nil, nil)

		assert.Contains(t, registry.ListAvailableTools(), "TextToMusic")

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == "TextToMusic" {
				found = true
			}
		}
		assert.True(t, found, "TextToMusic definition should be in the tools payload")
	})
}
