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

	// ensureBinary, when set, downloads a prebuilt ffmpeg into ~/.infer/bin as
	// a last resort after config-path and PATH resolution fail (see
	// stt.BinaryManager, injected via SetBinaryEnsurer to avoid an import cycle).
	ensureBinary func(ctx context.Context, name string) (string, error)

	// run and lookPath are overridable in tests.
	run      commandRunner
	lookPath func(string) (string, error)
}

// SetBinaryEnsurer installs a fallback that resolves (downloading if needed) a
// named binary when it is not found on PATH.
func (c *Converter) SetBinaryEnsurer(f func(ctx context.Context, name string) (string, error)) {
	c.ensureBinary = f
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
	if err != nil && c.ensureBinary != nil {
		if path, dlErr := c.ensureBinary(ctx, "ffmpeg"); dlErr == nil {
			ffmpeg, err = path, nil
		}
	}
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
