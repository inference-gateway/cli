package stt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func found(name string) (string, error) { return "/usr/bin/" + name, nil }
func notFound(string) (string, error)   { return "", errors.New("not found") }

func TestResolveBinaryConfiguredPath(t *testing.T) {
	w := NewWhisperTranscriber(config.SpeechToTextConfig{BinaryPath: "/opt/whisper"})
	w.lookPath = func(name string) (string, error) {
		if name == "/opt/whisper" {
			return name, nil
		}
		return "", errors.New("not found")
	}
	got, err := w.resolveBinary(context.Background())
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "/opt/whisper" {
		t.Errorf("resolveBinary = %q, want /opt/whisper", got)
	}
}

func TestResolveBinaryCandidateFallback(t *testing.T) {
	w := NewWhisperTranscriber(config.SpeechToTextConfig{})
	w.lookPath = func(name string) (string, error) {
		if name == "whisper-cpp" {
			return "/usr/bin/whisper-cpp", nil
		}
		return "", errors.New("not found")
	}
	got, err := w.resolveBinary(context.Background())
	if err != nil {
		t.Fatalf("resolveBinary: %v", err)
	}
	if got != "whisper-cpp" {
		t.Errorf("resolveBinary = %q, want whisper-cpp", got)
	}
}

func TestResolveBinaryNotFound(t *testing.T) {
	w := NewWhisperTranscriber(config.SpeechToTextConfig{})
	w.lookPath = notFound
	_, err := w.resolveBinary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "whisper binary not found") {
		t.Fatalf("expected 'whisper binary not found' error, got %v", err)
	}
}

func TestEnsureAvailable(t *testing.T) {
	w := NewWhisperTranscriber(config.SpeechToTextConfig{})
	w.lookPath = found
	if err := w.EnsureAvailable(); err != nil {
		t.Errorf("expected available, got %v", err)
	}
	w.lookPath = notFound
	if err := w.EnsureAvailable(); err == nil {
		t.Error("expected error when whisper binary is missing")
	}
}

func TestTranscribeSuccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ggml-tiny.bin"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := NewWhisperTranscriber(config.SpeechToTextConfig{
		Model: "tiny", ModelsDir: dir, Language: "en",
	})
	w.lookPath = found
	var gotArgs []string
	w.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		gotArgs = args
		return []byte("  Hello \n\n world \n"), nil
	}

	text, err := w.Transcribe(context.Background(), "/tmp/audio.wav")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if text != "Hello world" {
		t.Errorf("Transcribe = %q, want %q", text, "Hello world")
	}
	joined := strings.Join(gotArgs, " ")
	for _, want := range []string{"-nt", "-np", "-l en", "-f /tmp/audio.wav"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}
}

func TestTranscribeBinaryMissing(t *testing.T) {
	w := NewWhisperTranscriber(config.SpeechToTextConfig{Model: "tiny"})
	w.lookPath = notFound
	w.run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		t.Fatal("run should not be called when binary is missing")
		return nil, nil
	}
	if _, err := w.Transcribe(context.Background(), "/tmp/audio.wav"); err == nil {
		t.Fatal("expected error when whisper binary is missing")
	}
}

func TestCleanTranscript(t *testing.T) {
	cases := map[string]string{
		"":                     "",
		"  hello  ":            "hello",
		"line one\nline two":   "line one line two",
		"\n\nfoo\n\nbar\n":     "foo bar",
		"single":               "single",
		"[BLANK_AUDIO]":        "",
		"[ Silence ]":          "",
		"(blank_audio)":        "",
		"[MUSIC]":              "",
		"hello [MUSIC] world":  "hello world",
		"[BLANK_AUDIO] hi all": "hi all",
	}
	for in, want := range cases {
		if got := cleanTranscript(in); got != want {
			t.Errorf("cleanTranscript(%q) = %q, want %q", in, got, want)
		}
	}
}
