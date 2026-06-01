package audio

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	config "github.com/inference-gateway/cli/config"
)

// Converter converts arbitrary audio files into Whisper-ready 16kHz mono WAV.
// It is used to turn Telegram voice notes (OGG/Opus) into a format whisper.cpp
// can read.
type Converter struct {
	ffmpegPath string

	// run and lookPath are overridable in tests.
	run      commandRunner
	lookPath func(string) (string, error)
}

// NewConverter creates a Converter from the speech-to-text config.
func NewConverter(cfg config.SpeechToTextConfig) *Converter {
	return &Converter{
		ffmpegPath: cfg.FFmpegPath,
		run:        execRun,
		lookPath:   exec.LookPath,
	}
}

// ToWhisperWAV converts srcPath into a new 16kHz mono WAV file and returns its
// path. The caller owns the returned file and should remove it when done.
func (c *Converter) ToWhisperWAV(ctx context.Context, srcPath string) (string, error) {
	ffmpeg, err := resolveFFmpeg(c.ffmpegPath, c.lookPath)
	if err != nil {
		return "", err
	}

	out, err := tempWAV()
	if err != nil {
		return "", err
	}

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-i", srcPath,
		"-ar", "16000", "-ac", "1", "-f", "wav", out,
	}
	if _, err := c.run(ctx, ffmpeg, args...); err != nil {
		_ = os.Remove(out)
		return "", fmt.Errorf("ffmpeg audio conversion failed: %w", err)
	}
	return out, nil
}
