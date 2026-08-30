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

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// fakeVoiceSynthesizer is a hand-written fake for the consumer-side
// voiceSynthesizer interface, mirroring the FakeImageService usage.
type fakeVoiceSynthesizer struct {
	err    error
	text   string
	sample string
	out    string
	calls  int
}

func (f *fakeVoiceSynthesizer) Synthesize(ctx context.Context, text, voiceSamplePath, outPath string) error {
	f.calls++
	f.text, f.sample, f.out = text, voiceSamplePath, outPath
	if f.err != nil {
		return f.err
	}
	// Produce a short valid WAV so duration parsing has something to read.
	data := make([]byte, 48000) // 1s of 24kHz 16-bit mono
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
	return os.WriteFile(outPath, buf.Bytes(), 0o644)
}

func newTestTTSTool(t *testing.T, enabled bool, synth voiceSynthesizer) *TextToSpeechTool {
	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToSpeech.Enabled = enabled
	cfg.TextToSpeech.OutputDir = t.TempDir()
	return NewTextToSpeechTool(cfg, synth)
}

func TestTextToSpeechTool_IsEnabled(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		engine   string
		expected bool
	}{
		{"enabled with default engine", true, "", true},
		{"enabled with qwen3-tts engine", true, "qwen3-tts", true},
		{"unsupported engine", true, "piper", false},
		{"disabled in config", false, "qwen3-tts", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := newTestTTSTool(t, tt.enabled, &fakeVoiceSynthesizer{})
			tool.config.TextToSpeech.Engine = tt.engine
			assert.Equal(t, tt.expected, tool.IsEnabled())
		})
	}
}

func TestTextToSpeechTool_Definition(t *testing.T) {
	tool := newTestTTSTool(t, true, &fakeVoiceSynthesizer{})
	def := tool.Definition()

	assert.Equal(t, "TextToSpeech", def.Function.Name)
	assert.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "WAV")

	params := *def.Function.Parameters
	assert.Equal(t, []string{"text"}, params["required"])
	properties := params["properties"].(map[string]any)
	assert.Contains(t, properties, "text")
	assert.Contains(t, properties, "voice_sample")
	assert.Contains(t, properties, "output_path")
}

func TestTextToSpeechTool_Validate(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	tool := newTestTTSTool(t, true, &fakeVoiceSynthesizer{})

	if err := os.WriteFile("speaker.wav", []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"text only", map[string]any{"text": "hello"}, false},
		{"bare voice sample file", map[string]any{"text": "hello", "voice_sample": "speaker.wav"}, false},
		{"bare output path", map[string]any{"text": "hello", "output_path": "out.wav"}, false},
		{"missing text", map[string]any{}, true},
		{"empty text", map[string]any{"text": "  "}, true},
		{"missing voice sample file", map[string]any{"text": "hello", "voice_sample": "nope.wav"}, true},
		{"voice sample traversal", map[string]any{"text": "hello", "voice_sample": "../escape.wav"}, true},
		{"voice sample absolute", map[string]any{"text": "hello", "voice_sample": "/etc/passwd"}, true},
		{"voice sample nested", map[string]any{"text": "hello", "voice_sample": "sub/dir/speaker.wav"}, true},
		{"voice sample directory", map[string]any{"text": "hello", "voice_sample": "."}, true},
		{"output path traversal", map[string]any{"text": "hello", "output_path": "../escape.wav"}, true},
		{"output path absolute", map[string]any{"text": "hello", "output_path": "/tmp/out.wav"}, true},
		{"output path nested", map[string]any{"text": "hello", "output_path": "sub/out.wav"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			assert.Equal(t, tt.wantErr, err != nil, "err = %v", err)
		})
	}
}

func TestTextToSpeechTool_ExecuteStockVoice(t *testing.T) {
	fake := &fakeVoiceSynthesizer{}
	tool := newTestTTSTool(t, true, fake)
	outName := "speech.wav"

	result, err := tool.Execute(context.Background(), map[string]any{"text": "hello there", "output_path": outName})

	require.NoError(t, err)
	require.True(t, result.Success)
	outPath := filepath.Join(tool.config.TextToSpeech.OutputDir, outName)
	assert.Equal(t, outPath, fake.out)
	assert.Equal(t, "hello there", fake.text)
	assert.Empty(t, fake.sample)

	data := result.Data.(map[string]any)
	assert.Equal(t, outPath, data["path"])
	assert.Greater(t, data["duration_seconds"].(float64), 0.0)
	assert.Equal(t, false, data["voice_cloned"])
}

func TestTextToSpeechTool_ExecuteVoiceClone(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	fake := &fakeVoiceSynthesizer{}
	tool := newTestTTSTool(t, true, fake)
	if err := os.WriteFile("speaker.wav", []byte("wav"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := tool.Execute(context.Background(), map[string]any{
		"text": "hello", "voice_sample": "speaker.wav",
	})

	require.NoError(t, err)
	require.True(t, result.Success)
	assert.Equal(t, filepath.Join(tmp, "speaker.wav"), fake.sample)

	data := result.Data.(map[string]any)
	assert.Equal(t, true, data["voice_cloned"])
	assert.True(t, strings.HasSuffix(data["path"].(string), ".wav"))
}

func TestTextToSpeechTool_ExecuteDefaultOutputPath(t *testing.T) {
	fake := &fakeVoiceSynthesizer{}
	tool := newTestTTSTool(t, true, fake)

	result, err := tool.Execute(context.Background(), map[string]any{"text": "hello"})

	require.NoError(t, err)
	require.True(t, result.Success)
	outDir := tool.config.TextToSpeech.OutputDir
	assert.True(t, strings.HasPrefix(fake.out, outDir), "output %s should live under %s", fake.out, outDir)
	assert.True(t, strings.HasSuffix(fake.out, ".wav"))
	assert.FileExists(t, fake.out)
}

func TestTextToSpeechTool_ExecuteSynthesisFailure(t *testing.T) {
	fake := &fakeVoiceSynthesizer{err: fmt.Errorf("engine crashed")}
	tool := newTestTTSTool(t, true, fake)

	result, err := tool.Execute(context.Background(), map[string]any{"text": "hello", "output_path": "out.wav"})

	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.Contains(t, result.Error, "engine crashed")
}

func TestTextToSpeechTool_ExecuteInvalidArgs(t *testing.T) {
	fake := &fakeVoiceSynthesizer{}
	tool := newTestTTSTool(t, true, fake)

	_, err := tool.Execute(context.Background(), map[string]any{})

	assert.Error(t, err)
	assert.Zero(t, fake.calls)
}

func TestTextToSpeechTool_FormatPreview(t *testing.T) {
	tool := newTestTTSTool(t, true, &fakeVoiceSynthesizer{})

	assert.Equal(t, "Speech synthesis failed", tool.FormatPreview(nil))
	assert.Equal(t, "Speech saved to /tmp/x.wav",
		tool.FormatPreview(ttsResult("/tmp/x.wav")))
	assert.Equal(t, "Speech synthesis failed",
		tool.FormatPreview(&agentdomain.ToolExecutionResult{Success: false, Error: "engine crashed"}))
}

func TestTextToSpeechTool_FormatForLLM(t *testing.T) {
	tool := newTestTTSTool(t, true, &fakeVoiceSynthesizer{})

	formatted := tool.FormatForLLM(ttsResult("/tmp/x.wav"))
	assert.Contains(t, formatted, "Speech saved to /tmp/x.wav")
	assert.Contains(t, formatted, "2.5s of audio")

	failed := &agentdomain.ToolExecutionResult{ToolName: "TextToSpeech", Success: false, Error: "boom"}
	assert.Equal(t, "Speech synthesis failed: boom", tool.FormatForLLM(failed))
}

// ttsResult builds a success result with a path, mirroring Execute output.
func ttsResult(path string) *agentdomain.ToolExecutionResult {
	return &agentdomain.ToolExecutionResult{
		ToolName: "TextToSpeech",
		Success:  true,
		Data: map[string]any{
			"path":             path,
			"duration_seconds": 2.5,
		},
	}
}

// TestRegistryTextToSpeechGating pins the core acceptance criterion: with the
// feature disabled (the default) the tool is not registered at all, so its
// definition never reaches the tools payload sent to the LLM.
func TestTextToSpeechTool_RegistryGating(t *testing.T) {
	t.Run("disabled by default: not registered", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToSpeech.Enabled = false
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil)

		assert.NotContains(t, registry.ListAvailableTools(), "TextToSpeech")
		for _, def := range registry.GetToolDefinitions() {
			assert.NotEqual(t, "TextToSpeech", def.Function.Name)
		}
		_, err := registry.GetTool("TextToSpeech")
		assert.Error(t, err)
	})

	t.Run("enabled: present in the tools payload", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.TextToSpeech.Enabled = true
		registry := NewRegistry(cfg, nil, nil, nil, nil, nil, nil)

		assert.Contains(t, registry.ListAvailableTools(), "TextToSpeech")

		var found bool
		for _, def := range registry.GetToolDefinitions() {
			if def.Function.Name == "TextToSpeech" {
				found = true
			}
		}
		assert.True(t, found, "TextToSpeech definition should be in the tools payload")
	})
}
