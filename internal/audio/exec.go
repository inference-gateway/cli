// Package audio provides CGO-free microphone capture and audio-format
// conversion by shelling out to ffmpeg (with arecord/sox fallbacks on Linux).
// It produces 16kHz mono WAV, the input format whisper.cpp expects, and is used
// by the chat /voice shortcut and Telegram voice-message transcription.
package audio

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// commandRunner runs a command and returns its stdout, abstracting exec for tests.
type commandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

// candidate is a single external-tool invocation (command name + fixed args).
// silence marks an ffmpeg invocation whose args enable silencedetect, so the
// recorder stops it shortly after the speaker goes quiet.
type candidate struct {
	name    string
	args    []string
	silence bool
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

// ffmpegName returns the ffmpeg binary name, honoring an explicit configured path.
func ffmpegName(configured string) string {
	if c := strings.TrimSpace(configured); c != "" {
		return c
	}
	return "ffmpeg"
}

// resolveFFmpeg returns the ffmpeg binary to use or an actionable error.
func resolveFFmpeg(configured string, lookPath func(string) (string, error)) (string, error) {
	name := ffmpegName(configured)
	if _, err := lookPath(name); err == nil {
		return name, nil
	}
	if c := strings.TrimSpace(configured); c != "" {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("ffmpeg not found: install ffmpeg (e.g. `brew install ffmpeg`, " +
		"`apt install ffmpeg`) or set speech_to_text.ffmpeg_path")
}

// tempWAV creates an empty temp .wav file and returns its path.
func tempWAV() (string, error) {
	f, err := os.CreateTemp("", "infer-stt-*.wav")
	if err != nil {
		return "", fmt.Errorf("creating temp wav file: %w", err)
	}
	name := f.Name()
	_ = f.Close()
	return name, nil
}
