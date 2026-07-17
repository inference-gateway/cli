// Package stt provides CGO-free speech-to-text by shelling out to a local
// whisper.cpp binary (whisper-cli / whisper-cpp) and downloading GGML models on
// demand. It is gated by config.SpeechToTextConfig and used by the chat /voice
// shortcut and by Telegram voice-message transcription.
package stt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// nonSpeechMarkerRe matches whisper.cpp's bracketed non-speech annotations (e.g.
// "[BLANK_AUDIO]", "[ Silence ]", "(music)") so they are not inserted as text.
var nonSpeechMarkerRe = regexp.MustCompile(
	`(?i)[\[(<]\s*(blank[ _]?audio|silence|silent|music|noise|inaudible|pause|sound|applause|laughter|no[ _]?speech)\s*[\])>]`)

// whisperBinaryCandidates are the binary names tried, in order, when no explicit
// speech_to_text.binary_path is configured.
var whisperBinaryCandidates = []string{"whisper-cli", "whisper-cpp"}

// commandRunner runs a command and returns its stdout, abstracting exec for tests.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// WhisperTranscriber transcribes a 16kHz mono WAV file using whisper.cpp.
type WhisperTranscriber struct {
	cfg      config.SpeechToTextConfig
	models   *ModelManager
	binaries *BinaryManager

	// run and lookPath are overridable in tests.
	run      commandRunner
	lookPath func(string) (string, error)
}

// NewWhisperTranscriber creates a transcriber from the speech-to-text config.
func NewWhisperTranscriber(cfg config.SpeechToTextConfig) *WhisperTranscriber {
	return &WhisperTranscriber{
		cfg:      cfg,
		models:   NewModelManager(cfg),
		binaries: NewBinaryManager(cfg),
		run:      execRun,
		lookPath: exec.LookPath,
	}
}

// EnsureAvailable reports whether the whisper binary can be resolved (possibly
// by downloading it), without transcribing or downloading a model. It lets
// callers fail fast (with an actionable install hint) before recording audio.
func (w *WhisperTranscriber) EnsureAvailable() error {
	_, err := w.resolveBinary(context.Background())
	return err
}

// Transcribe converts the audio at wavPath (16kHz mono WAV) into text.
func (w *WhisperTranscriber) Transcribe(ctx context.Context, wavPath string) (string, error) {
	bin, err := w.resolveBinary(ctx)
	if err != nil {
		return "", err
	}

	modelPath, err := w.models.EnsureModel(ctx)
	if err != nil {
		return "", err
	}

	timeout := w.cfg.Timeout
	if timeout <= 0 {
		timeout = 120
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	args := []string{"-m", modelPath, "-f", wavPath, "-nt", "-np"}
	if lang := strings.TrimSpace(w.cfg.Language); lang != "" {
		args = append(args, "-l", lang)
	}

	out, err := w.run(ctx, bin, args...)
	if err != nil {
		return "", fmt.Errorf("whisper transcription failed: %w", err)
	}
	return cleanTranscript(string(out)), nil
}

// resolveBinary returns the whisper binary to invoke: an explicit configured
// path first, then PATH lookup of the known names, then a prebuilt binary in
// ~/.infer/bin (downloaded on first use when auto_download is enabled).
func (w *WhisperTranscriber) resolveBinary(ctx context.Context) (string, error) {
	if p := strings.TrimSpace(w.cfg.BinaryPath); p != "" {
		if _, err := w.lookPath(p); err == nil {
			return p, nil
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("configured speech_to_text.binary_path %q not found or not executable", p)
	}

	for _, name := range whisperBinaryCandidates {
		if _, err := w.lookPath(name); err == nil {
			return name, nil
		}
	}

	if w.cfg.AutoDownload && w.binaries != nil {
		if path, err := w.binaries.EnsureBinary(ctx, "whisper-cli"); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("whisper binary not found (tried %s): install whisper.cpp "+
		"(e.g. `brew install whisper-cpp`, nix `openai-whisper-cpp`, or build from "+
		"https://github.com/ggml-org/whisper.cpp) or set speech_to_text.binary_path",
		strings.Join(whisperBinaryCandidates, ", "))
}

// cleanTranscript normalizes whisper.cpp stdout into a single trimmed string,
// stripping non-speech markers (e.g. "[BLANK_AUDIO]") and collapsing whitespace.
// A recording with no detected speech therefore yields an empty string.
func cleanTranscript(out string) string {
	out = nonSpeechMarkerRe.ReplaceAllString(out, " ")
	return strings.Join(strings.Fields(out), " ")
}

// execRun runs name with args and returns stdout, wrapping failures with stderr.
func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, msg)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
