package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// defaultSynthesisTimeoutSeconds is the fallback when text_to_speech.timeout is
// unset; DefaultConfig sets the same value for a config written to disk.
const defaultSynthesisTimeoutSeconds = 300

// maxVoiceSampleSeconds caps the ffmpeg-normalized voice sample length so a
// long reference recording cannot balloon cloning time.
const maxVoiceSampleSeconds = 30

// Synthesizer synthesizes speech to a WAV file using a local llama.cpp
// llama-tts binary running Qwen3-TTS GGUF models. An empty voice sample uses
// the stock voice; a reference WAV enables zero-shot voice cloning.
type Synthesizer struct {
	cfg      config.TextToSpeechConfig
	models   *TTSModelManager
	binaries *BinaryManager

	run      commandRunner
	lookPath func(string) (string, error)
}

// NewSynthesizer creates a synthesizer from the text-to-speech config.
func NewSynthesizer(cfg config.TextToSpeechConfig) *Synthesizer {
	binaries := NewBinaryManager(config.SpeechToTextConfig{AutoDownload: cfg.AutoDownload})
	return &Synthesizer{
		cfg:      cfg,
		models:   NewTTSModelManager(cfg),
		binaries: binaries,
		run:      execRun,
		lookPath: exec.LookPath,
	}
}

// Synthesize converts text into a WAV at outPath, using the stock voice when
// voiceSamplePath is empty and cloning the supplied voice otherwise.
func (s *Synthesizer) Synthesize(ctx context.Context, text, voiceSamplePath, outPath string) error {
	bin, err := s.resolveBinary(ctx)
	if err != nil {
		return err
	}

	backbone, mmproj, err := s.models.EnsureModels(ctx)
	if err != nil {
		return err
	}

	resolvedOutPath, err := filepath.Abs(filepath.Clean(outPath))
	if err != nil {
		return fmt.Errorf("resolving output path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(resolvedOutPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	timeout := s.cfg.Timeout
	if timeout <= 0 {
		timeout = defaultSynthesisTimeoutSeconds
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	sample := strings.TrimSpace(voiceSamplePath)
	if sample != "" {
		normalized, err := s.normalizeVoiceSample(ctx, sample)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(normalized) }()
		sample = normalized
	}

	args := []string{"-m", backbone, "-mm", mmproj, "-p", text}
	if sample != "" {
		args = append(args, "--tts-speaker-file", sample)
	}
	args = append(args, "-o", resolvedOutPath)

	if _, err := s.run(ctx, bin, args...); err != nil {
		return fmt.Errorf("tts synthesis failed: %w", err)
	}
	return nil
}

// resolveBinary returns the configured llama-tts binary, finds it on PATH, or
// auto-downloads it from the binaries release into ~/.infer/bin — the same
// resolution order and cache the gateway's local engine uses.
func (s *Synthesizer) resolveBinary(ctx context.Context) (string, error) {
	if p := strings.TrimSpace(s.cfg.BinaryPath); p != "" {
		if _, err := s.lookPath(p); err == nil {
			return p, nil
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
		return "", fmt.Errorf("configured text_to_speech.binary_path %q not found or not executable", p)
	}

	if _, err := s.lookPath("llama-tts"); err == nil {
		return "llama-tts", nil
	}

	if s.cfg.AutoDownload {
		path, err := s.binaries.EnsureBinary(ctx, "llama-tts")
		if err != nil {
			return "", fmt.Errorf("downloading llama-tts: %w", err)
		}
		return path, nil
	}

	return "", fmt.Errorf("llama-tts binary not found and text_to_speech.auto_download is disabled: " +
		"enable auto_download, install llama.cpp with TTS support (e.g. `brew install llama.cpp`), " +
		"or set text_to_speech.binary_path")
}

// normalizeVoiceSample converts the reference into a 16kHz mono WAV capped at
// maxVoiceSampleSeconds using ffmpeg.
func (s *Synthesizer) normalizeVoiceSample(ctx context.Context, srcPath string) (string, error) {
	ffmpeg, err := resolveFFmpeg(s.cfg.FFmpegPath, s.lookPath)
	if err != nil && s.cfg.AutoDownload {
		if path, dlErr := s.binaries.EnsureBinary(ctx, "ffmpeg"); dlErr == nil {
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
		"-t", strconv.Itoa(maxVoiceSampleSeconds),
		"-ar", "16000", "-ac", "1", "-f", "wav", out,
	}
	if _, err := s.run(ctx, ffmpeg, args...); err != nil {
		_ = os.Remove(out)
		return "", fmt.Errorf("ffmpeg voice sample normalization failed: %w", err)
	}

	if d, err := WAVDurationSeconds(out); err == nil && d < 1 {
		_ = os.Remove(out)
		return "", fmt.Errorf("voice sample %s is too short to clone a voice (%.1fs); "+
			"provide at least a few seconds of speech, ideally 10-30 seconds of clean speech",
			srcPath, d)
	}
	return out, nil
}

// WAVDurationSeconds returns the duration of a RIFF/WAVE file in seconds by
// reading its fmt and data chunk headers. It lets callers report the duration
// of synthesized speech without external tools.
func WAVDurationSeconds(path string) (float64, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return 0, fmt.Errorf("opening wav: %w", err)
	}
	defer func() { _ = f.Close() }()

	header := make([]byte, 12)
	if _, err := io.ReadFull(f, header); err != nil {
		return 0, fmt.Errorf("reading wav header: %w", err)
	}
	if string(header[0:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return 0, fmt.Errorf("not a WAV file: %s", path)
	}

	var (
		byteRate uint32
		dataSize uint64
	)
	chunk := make([]byte, 8)
	for {
		if _, err := io.ReadFull(f, chunk); err != nil {
			return 0, fmt.Errorf("reading wav chunks: %w", err)
		}
		size := binary.LittleEndian.Uint32(chunk[4:8])
		switch string(chunk[0:4]) {
		case "fmt ":
			if size < 16 || size > 4096 {
				return 0, fmt.Errorf("invalid wav fmt chunk size %d: %s", size, path)
			}
			fmtChunk := make([]byte, size)
			if _, err := io.ReadFull(f, fmtChunk); err != nil {
				return 0, fmt.Errorf("reading wav fmt chunk: %w", err)
			}
			byteRate = binary.LittleEndian.Uint32(fmtChunk[8:12])
		case "data":
			dataSize = uint64(size)
			if byteRate == 0 || dataSize == 0 {
				return 0, fmt.Errorf("wav file lacks sample data: %s", path)
			}
			return float64(dataSize) / float64(byteRate), nil
		default:
			if _, err := io.CopyN(io.Discard, f, int64(size)); err != nil {
				return 0, fmt.Errorf("skipping wav chunk: %w", err)
			}
		}
	}
}
