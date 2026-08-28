package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestTTSModelFiles(t *testing.T) {
	tests := []struct {
		model        string
		wantBackbone string
		wantMmproj   string
	}{
		{"", "Qwen3-TTS-12Hz-1.7B-Base-Q4_K_M.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf"},
		{"base", "Qwen3-TTS-12Hz-1.7B-Base-Q4_K_M.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf"},
		{"q8", "Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf"},
		{"bf16", "Qwen3-TTS-12Hz-1.7B-Base-bf16.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-bf16.gguf"},
		{"Custom-Backbone-Q8_0.gguf", "Custom-Backbone-Q8_0.gguf", "mmproj-Custom-Backbone-Q8_0.gguf"},
		{"a.gguf,b.gguf", "a.gguf", "b.gguf"},
	}
	for _, tt := range tests {
		backbone, mmproj := ttsModelFiles(tt.model)
		if backbone != tt.wantBackbone || mmproj != tt.wantMmproj {
			t.Errorf("ttsModelFiles(%q) = (%q, %q), want (%q, %q)",
				tt.model, backbone, mmproj, tt.wantBackbone, tt.wantMmproj)
		}
	}
}

func TestResolveTTSBinaryConfiguredPath(t *testing.T) {
	s := NewSynthesizer(config.TextToSpeechConfig{BinaryPath: "/opt/llama-tts"})
	s.lookPath = func(name string) (string, error) {
		if name == "/opt/llama-tts" {
			return name, nil
		}
		return "", errors.New("not found")
	}
	got, err := s.resolveBinary(context.Background())
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "/opt/llama-tts" {
		t.Errorf("resolveBinary = %q, want /opt/llama-tts", got)
	}
}

func TestResolveTTSBinaryCandidateFallback(t *testing.T) {
	s := NewSynthesizer(config.TextToSpeechConfig{})
	s.lookPath = func(name string) (string, error) {
		if name == "llama-tts" {
			return "/usr/bin/llama-tts", nil
		}
		return "", errors.New("not found")
	}
	got, err := s.resolveBinary(context.Background())
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "llama-tts" {
		t.Errorf("resolveBinary = %q, want llama-tts", got)
	}
}

func TestResolveTTSBinaryNotFound(t *testing.T) {
	s := NewSynthesizer(config.TextToSpeechConfig{})
	s.lookPath = notFound
	_, err := s.resolveBinary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "llama-tts binary not found") {
		t.Fatalf("expected 'llama-tts binary not found' error, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "text_to_speech.binary_path") {
		t.Errorf("error should name the config escape hatch, got %v", err)
	}
}

func TestEnsureTTSAvailable(t *testing.T) {
	s := NewSynthesizer(config.TextToSpeechConfig{})
	s.lookPath = found
	if err := s.EnsureAvailable(); err != nil {
		t.Errorf("expected available, got %v", err)
	}
	s.lookPath = notFound
	if err := s.EnsureAvailable(); err == nil {
		t.Error("expected error when llama-tts is missing")
	}
}

// synthTestEnv prepares a synthesizer against a temp models dir with both GGUF
// files pre-placed (auto_download disabled), a working binary lookup, and a
// fake runner that records invocations and writes a valid WAV to the -o target.
func synthTestEnv(t *testing.T) (*Synthesizer, *[]string, func() []string) {
	t.Helper()

	dir := t.TempDir()
	backbone, mmproj := ttsModelFiles("")
	for _, name := range []string{backbone, mmproj} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("gguf"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := NewSynthesizer(config.TextToSpeechConfig{
		ModelsDir: dir, AutoDownload: false, Timeout: 5,
	})
	s.lookPath = found

	var calls []string
	s.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		joined := name + " " + strings.Join(args, " ")
		calls = append(calls, joined)
		if strings.Contains(name, "ffmpeg") {
			if err := writeTestWAV(filepath.Join(args[len(args)-1]), 16000, 1.5); err != nil {
				t.Fatal(err)
			}
			return []byte{}, nil
		}
		return []byte{}, nil
	}
	return s, &calls, func() []string { return calls }
}

func TestSynthesizeStockVoice(t *testing.T) {
	s, _, getCalls := synthTestEnv(t)
	out := filepath.Join(t.TempDir(), "out.wav")

	if err := s.Synthesize(context.Background(), "Hello world", "", out); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	calls := getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 engine invocation, got %d: %v", len(calls), calls)
	}
	if strings.Contains(calls[0], "--tts-speaker-file") {
		t.Errorf("stock voice must not pass --tts-speaker-file: %v", calls[0])
	}
	for _, want := range []string{"-p Hello world", "-o " + out, "-m ", "-mm "} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("engine args %q missing %q", calls[0], want)
		}
	}
}

func TestSynthesizeVoiceClone(t *testing.T) {
	s, _, getCalls := synthTestEnv(t)
	sample := filepath.Join(t.TempDir(), "speaker.wav")
	if err := writeTestWAV(sample, 44100, 2); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.wav")

	if err := s.Synthesize(context.Background(), "Hello world", sample, out); err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	calls := getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected ffmpeg normalization + engine invocation, got %d: %v", len(calls), calls)
	}
	if !strings.Contains(calls[0], "ffmpeg") {
		t.Fatalf("expected ffmpeg call first, got %q", calls[0])
	}
	for _, want := range []string{"-ar 16000", "-ac 1", "-t 30"} {
		if !strings.Contains(calls[0], want) {
			t.Errorf("normalization args %q missing %q", calls[0], want)
		}
	}
	if !strings.Contains(calls[1], "--tts-speaker-file") {
		t.Errorf("voice clone must pass --tts-speaker-file, got %q", calls[1])
	}

	// The normalized temp sample is removed after synthesis.
	var samplePath string
	for _, call := range calls {
		fields := strings.Fields(call)
		for i, arg := range fields {
			if arg == "--tts-speaker-file" && i+1 < len(fields) {
				samplePath = fields[i+1]
			}
		}
	}
	if samplePath == "" {
		t.Fatal("could not extract the normalized sample path")
	}
	if _, err := os.Stat(samplePath); !os.IsNotExist(err) {
		t.Errorf("normalized sample %s should be removed after synthesis", samplePath)
	}
}

func TestSynthesizeBinaryMissing(t *testing.T) {
	s, _, _ := synthTestEnv(t)
	s.lookPath = notFound
	if err := s.Synthesize(context.Background(), "Hello", "", filepath.Join(t.TempDir(), "out.wav")); err == nil {
		t.Fatal("expected error when llama-tts is missing")
	} else if !strings.Contains(err.Error(), "llama-tts binary not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSynthesizeModelMissing(t *testing.T) {
	s := NewSynthesizer(config.TextToSpeechConfig{
		ModelsDir: t.TempDir(), AutoDownload: false,
	})
	s.lookPath = found
	s.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("run should not be called when models are missing")
		return nil, nil
	}
	err := s.Synthesize(context.Background(), "Hello", "", filepath.Join(t.TempDir(), "out.wav"))
	if err == nil || !strings.Contains(err.Error(), "auto_download is disabled") {
		t.Fatalf("expected actionable missing-model error, got %v", err)
	}
}

func TestWAVDurationSeconds(t *testing.T) {
	t.Run("valid wav", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "a.wav")
		if err := writeTestWAV(path, 24000, 2.0); err != nil {
			t.Fatal(err)
		}
		got, err := WAVDurationSeconds(path)
		if err != nil {
			t.Fatalf("WAVDurationSeconds: %v", err)
		}
		if got < 1.99 || got > 2.01 {
			t.Errorf("duration = %v, want ~2.0", got)
		}
	})

	t.Run("not a wav", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "b.wav")
		if err := os.WriteFile(path, []byte("not a wave"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := WAVDurationSeconds(path); err == nil {
			t.Error("expected error for non-WAV content")
		}
	})
}

// writeTestWAV writes a minimal valid 16-bit mono PCM WAV.
func writeTestWAV(path string, sampleRate int, seconds float64) error {
	n := int(float64(sampleRate) * seconds * 2) // 16-bit mono
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+n))
	buf.WriteString("WAVEfmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))            // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))            // mono
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))   // sample rate
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2)) // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(2))            // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(16))           // bits per sample
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(n))
	buf.Write(make([]byte, n))
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
