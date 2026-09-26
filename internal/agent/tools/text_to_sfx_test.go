package tools

import (
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

// minimalWAV returns a tiny valid WAV: 1 second of 8 kHz mono 8-bit silence.
func minimalWAV() []byte {
	const dataBytes = 8000
	buf := make([]byte, 0, 44+dataBytes)
	buf = append(buf, "RIFF"...)
	buf = binary.LittleEndian.AppendUint32(buf, 36+dataBytes)
	buf = append(buf, "WAVE"...)
	buf = append(buf, "fmt "...)
	buf = binary.LittleEndian.AppendUint32(buf, 16)
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint32(buf, 8000)
	buf = binary.LittleEndian.AppendUint32(buf, 8000)
	buf = binary.LittleEndian.AppendUint16(buf, 1)
	buf = binary.LittleEndian.AppendUint16(buf, 8)
	buf = append(buf, "data"...)
	buf = binary.LittleEndian.AppendUint32(buf, dataBytes)
	return append(buf, make([]byte, dataBytes)...)
}

func newTestSFXTool(t *testing.T, enabled bool, sfx agentdomain.SoundEffectService) *TextToSFXTool {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToSFX.Enabled = enabled
	cfg.TextToSFX.OutputDir = t.TempDir()
	return NewTextToSFXTool(cfg, sfx)
}

func TestTextToSFXTool_IsEnabled(t *testing.T) {
	t.Run("enabled with a sound-effect service", func(t *testing.T) {
		tool := newTestSFXTool(t, true, &agentdomainmocks.FakeSoundEffectService{})
		assert.True(t, tool.IsEnabled())
	})
	t.Run("disabled in config", func(t *testing.T) {
		tool := newTestSFXTool(t, false, &agentdomainmocks.FakeSoundEffectService{})
		assert.False(t, tool.IsEnabled())
	})
	t.Run("enabled without a sound-effect service", func(t *testing.T) {
		tool := newTestSFXTool(t, true, nil)
		assert.False(t, tool.IsEnabled())
	})
}

func TestTextToSFXTool_Definition(t *testing.T) {
	tool := newTestSFXTool(t, true, &agentdomainmocks.FakeSoundEffectService{})
	def := tool.Definition()

	assert.Equal(t, "TextToSFX", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "sound")

	params := *def.Function.Parameters
	assert.Equal(t, []string{"prompt"}, params["required"])
	properties := params["properties"].(map[string]any)
	assert.Contains(t, properties, "prompt")
	assert.Contains(t, properties, "seconds")
	assert.Contains(t, properties, "loop")
	assert.Contains(t, properties, "output_path")
}

func TestTextToSFXTool_Validate(t *testing.T) {
	tool := newTestSFXTool(t, true, &agentdomainmocks.FakeSoundEffectService{})

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"prompt only", map[string]any{"prompt": "distant thunder"}, ""},
		{"with seconds", map[string]any{"prompt": "p", "seconds": 2.5}, ""},
		{"with loop", map[string]any{"prompt": "p", "loop": true}, ""},
		{"bare output path", map[string]any{"prompt": "p", "output_path": "whoosh.mp3"}, ""},
		{"missing prompt", map[string]any{}, "prompt is required"},
		{"empty prompt", map[string]any{"prompt": "  "}, "prompt is required"},
		{"non-string prompt", map[string]any{"prompt": 42}, "prompt is required"},
		{"non-number seconds", map[string]any{"prompt": "p", "seconds": "2"}, "seconds must be a number"},
		{"seconds below range", map[string]any{"prompt": "p", "seconds": 0.4}, "seconds must be between 0.5 and 30"},
		{"seconds above range", map[string]any{"prompt": "p", "seconds": 30.5}, "seconds must be between 0.5 and 30"},
		{"non-bool loop", map[string]any{"prompt": "p", "loop": "yes"}, "loop must be a boolean"},
		{"absolute output path", map[string]any{"prompt": "p", "output_path": "/tmp/whoosh.mp3"}, "invalid output_path"},
		{"output path with directory", map[string]any{"prompt": "p", "output_path": "sub/whoosh.mp3"}, "invalid output_path"},
		{"output path with ..", map[string]any{"prompt": "p", "output_path": "../whoosh.mp3"}, "invalid output_path"},
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

func TestTextToSFXTool_Execute(t *testing.T) {
	t.Run("reports path and prompt", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		sfx.GenerateStub = func(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error {
			return os.WriteFile(outPath, minimalWAV(), 0o644)
		}
		tool := newTestSFXTool(t, true, sfx)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":  "a short laser whoosh",
			"seconds": 2.5,
			"loop":    true,
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.True(t, res.Success)

		data, ok := res.Data.(map[string]any)
		require.True(t, ok)
		path, _ := data["path"].(string)
		assert.Contains(t, path, "sfx-")
		assert.Equal(t, "a short laser whoosh", data["prompt"])
		assert.True(t, strings.HasSuffix(path, ".mp3"))

		require.Equal(t, 1, sfx.GenerateCallCount())
		_, gotPrompt, gotOut, gotSeconds, gotLoop := sfx.GenerateArgsForCall(0)
		assert.Equal(t, "a short laser whoosh", gotPrompt)
		assert.Equal(t, filepath.Dir(gotOut), filepath.Dir(path))
		require.NotNil(t, gotSeconds)
		assert.InDelta(t, 2.5, *gotSeconds, 0.01)
		require.NotNil(t, gotLoop)
		assert.True(t, *gotLoop)
	})

	t.Run("named output creates the output directory", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		sfx.GenerateStub = func(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error {
			return os.WriteFile(outPath, minimalWAV(), 0o644)
		}
		tool := newTestSFXTool(t, true, sfx)
		tool.config.TextToSFX.OutputDir = filepath.Join(t.TempDir(), "fresh", "sfx")

		res, err := tool.Execute(context.Background(), map[string]any{"prompt": "room tone", "output_path": "room.mp3"})
		require.NoError(t, err)
		assert.True(t, res.Success, res.Error)
		assert.FileExists(t, filepath.Join(tool.config.TextToSFX.OutputDir, "room.mp3"))
	})

	t.Run("omitted knobs are nil", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		sfx.GenerateStub = func(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error {
			return os.WriteFile(outPath, minimalWAV(), 0o644)
		}
		tool := newTestSFXTool(t, true, sfx)

		_, err := tool.Execute(context.Background(), map[string]any{"prompt": "a click"})
		require.NoError(t, err)

		_, _, _, gotSeconds, gotLoop := sfx.GenerateArgsForCall(0)
		assert.Nil(t, gotSeconds)
		assert.Nil(t, gotLoop)
	})

	t.Run("provider failure reports the error and leaves no partial file", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		sfx.GenerateReturns(fmt.Errorf("provider does not support sfx"))
		tool := newTestSFXTool(t, true, sfx)

		res, err := tool.Execute(context.Background(), map[string]any{"prompt": "a riser"})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "does not support sfx")

		entries, readErr := os.ReadDir(tool.config.TextToSFX.OutputDir)
		require.NoError(t, readErr)
		assert.Empty(t, entries, "a failed call must not leave a partial file")
	})

	t.Run("failure with a named output leaves the caller's target untouched", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		sfx.GenerateReturns(fmt.Errorf("boom"))
		tool := newTestSFXTool(t, true, sfx)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "a riser",
			"output_path": "named.mp3",
		})
		require.NoError(t, err)
		require.NotNil(t, res)
		assert.False(t, res.Success)
		assert.NoFileExists(t, filepath.Join(tool.config.TextToSFX.OutputDir, "named.mp3"))
	})

	t.Run("invalid output_path fails without calling the service", func(t *testing.T) {
		sfx := &agentdomainmocks.FakeSoundEffectService{}
		tool := newTestSFXTool(t, true, sfx)

		res, err := tool.Execute(context.Background(), map[string]any{
			"prompt":      "a riser",
			"output_path": "/etc/passwd",
		})
		require.ErrorContains(t, err, "invalid output_path")
		assert.Nil(t, res)
		assert.Equal(t, 0, sfx.GenerateCallCount())
	})
}

func TestTextToSFXTool_Formatting(t *testing.T) {
	sfx := &agentdomainmocks.FakeSoundEffectService{}
	sfx.GenerateStub = func(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error {
		return os.WriteFile(outPath, minimalWAV(), 0o644)
	}
	tool := newTestSFXTool(t, true, sfx)

	res, err := tool.Execute(context.Background(), map[string]any{"prompt": "distant thunder"})
	require.NoError(t, err)

	assert.Contains(t, tool.FormatPreview(res), "Sound effect saved to")
	assert.Contains(t, tool.FormatForLLM(res), "Sound effect saved to")
	assert.False(t, strings.Contains(tool.FormatPreview(res), "\n"))
}

func TestTextToSFXTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToSFX.Enabled = false
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil, nil, nil, nil)

		assert.NotContains(t, registry.ListAvailableTools(), "TextToSFX")
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, "TextToSFX", def.Function.Name)
		}
		_, err := registry.GetTool("TextToSFX")
		assert.Error(t, err)
	})

	t.Run("enabled: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToSFX.Enabled = true
		cfg.TextToSFX.OutputDir = t.TempDir()
		registry := NewRegistry(cfg, nil, nil, nil, &agentdomainmocks.FakeSoundEffectService{}, nil, nil, nil, nil, nil)

		assert.Contains(t, registry.ListAvailableTools(), "TextToSFX")

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == "TextToSFX" {
				found = true
			}
		}
		assert.True(t, found, "TextToSFX definition should be in the tools payload")
	})
}
