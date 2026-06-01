package audio

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestToWhisperWAVSuccess(t *testing.T) {
	c := NewConverter(config.SpeechToTextConfig{})
	c.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	var gotArgs []string
	c.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return nil, nil
	}

	out, err := c.ToWhisperWAV(context.Background(), "/tmp/voice.ogg")
	if err != nil {
		t.Fatalf("ToWhisperWAV: %v", err)
	}
	defer func() { _ = os.Remove(out) }()

	args := strings.Join(gotArgs, " ")
	for _, want := range []string{"-i /tmp/voice.ogg", "-ar 16000", "-ac 1", "-f wav"} {
		if !strings.Contains(args, want) {
			t.Errorf("ffmpeg args %q missing %q", args, want)
		}
	}
	if !strings.HasSuffix(out, ".wav") {
		t.Errorf("ToWhisperWAV returned %q, want a .wav path", out)
	}
}

func TestToWhisperWAVFFmpegMissing(t *testing.T) {
	c := NewConverter(config.SpeechToTextConfig{})
	c.lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if _, err := c.ToWhisperWAV(context.Background(), "/tmp/voice.ogg"); err == nil ||
		!strings.Contains(err.Error(), "ffmpeg not found") {
		t.Fatalf("expected 'ffmpeg not found' error, got %v", err)
	}
}

func TestToWhisperWAVConversionError(t *testing.T) {
	c := NewConverter(config.SpeechToTextConfig{})
	c.lookPath = func(string) (string, error) { return "/usr/bin/ffmpeg", nil }
	c.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return nil, errors.New("boom")
	}
	if _, err := c.ToWhisperWAV(context.Background(), "/tmp/voice.ogg"); err == nil {
		t.Fatal("expected conversion error")
	}
}
