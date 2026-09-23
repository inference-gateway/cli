package audio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	huggingface "github.com/inference-gateway/cli/internal/platform/huggingface"
)

// qwen3TTSRepo hosts the default Qwen3-TTS GGUF models (backbone + mmproj),
// run through llama.cpp's llama-tts binary.
var qwen3TTSRepo = huggingface.Repo{ID: "ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF"}

// ttsQuantSuffixes are the quantization suffixes stripped from an explicit
// backbone filename to derive the paired mmproj filename.
var ttsQuantSuffixes = []string{"-Q4_K_M", "-Q8_0", "-Q4_0", "-bf16", "-F16"}

// ttsModelFiles returns the backbone and mmproj GGUF filenames for a preset or
// explicit model filename pair.
func ttsModelFiles(model string) (backbone, mmproj string) {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "base":
		return "Qwen3-TTS-12Hz-1.7B-Base-Q4_K_M.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf"
	case "q8", "base-q8":
		return "Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-Q8_0.gguf"
	case "bf16", "f16", "base-bf16":
		return "Qwen3-TTS-12Hz-1.7B-Base-bf16.gguf", "mmproj-Qwen3-TTS-12Hz-1.7B-Base-bf16.gguf"
	}

	parts := strings.Split(model, ",")
	backbone = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		return backbone, strings.TrimSpace(parts[1])
	}

	stem := strings.TrimSuffix(backbone, ".gguf")
	for _, q := range ttsQuantSuffixes {
		if stripped := strings.TrimSuffix(stem, q); stripped != stem {
			return backbone, fmt.Sprintf("mmproj-%s-Q8_0.gguf", stripped)
		}
	}
	return backbone, fmt.Sprintf("mmproj-%s-Q8_0.gguf", stem)
}

// TTSModelStore resolves and (optionally) downloads the TTS GGUF models
// (backbone + mmproj) into the models dir, mirroring ModelStore for whisper.
type TTSModelStore struct {
	cfg config.TextToSpeechConfig
	mu  sync.Mutex

	// hub is overridable in tests.
	hub *huggingface.Client
}

// NewTTSModelStore creates a TTSModelStore from the text-to-speech config.
func NewTTSModelStore(cfg config.TextToSpeechConfig) *TTSModelStore {
	return &TTSModelStore{
		cfg: cfg,
		hub: huggingface.NewClient(),
	}
}

// modelsDir returns the directory holding TTS models, defaulting to
// ~/.infer/models/tts when not configured.
func (m *TTSModelStore) modelsDir() (string, error) {
	if strings.TrimSpace(m.cfg.ModelsDir) != "" {
		return m.cfg.ModelsDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "models", "tts"), nil
}

// EnsureModels returns local paths to the backbone and mmproj GGUF files,
// downloading them on first use when AutoDownload is enabled. Concurrent
// callers are serialized so each model is downloaded once.
func (m *TTSModelStore) EnsureModels(ctx context.Context) (backbone, mmproj string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	backboneName, mmprojName := ttsModelFiles(m.cfg.Model)

	if backbone, err = m.ensureFile(ctx, backboneName); err != nil {
		return "", "", err
	}
	if mmproj, err = m.ensureFile(ctx, mmprojName); err != nil {
		return "", "", err
	}
	return backbone, mmproj, nil
}

// ensureFile returns the local path to the named GGUF file, downloading it on
// first use when AutoDownload is enabled.
func (m *TTSModelStore) ensureFile(ctx context.Context, name string) (string, error) {
	dir, err := m.modelsDir()
	if err != nil {
		return "", err
	}

	path, err := m.hub.EnsureFile(ctx, qwen3TTSRepo, name, dir, "tts model", m.cfg.AutoDownload, agentdomain.GetToolProgressCallback(ctx))
	if errors.Is(err, huggingface.ErrNotCached) {
		return "", fmt.Errorf("tts model %q not found at %s and text_to_speech.auto_download is disabled", name, filepath.Join(dir, name))
	}
	return path, err
}
