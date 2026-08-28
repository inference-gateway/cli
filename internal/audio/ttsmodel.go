package audio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// qwen3TTSBase is the resolve base for the default Qwen3-TTS GGUF models
// (backbone + mmproj), run through llama.cpp's llama-tts binary.
const qwen3TTSBase = "https://huggingface.co/ggml-org/Qwen3-TTS-12Hz-1.7B-Base-GGUF/resolve/main"

// ttsQuantSuffixes are the quantization suffixes stripped from an explicit
// backbone filename to derive the paired mmproj filename.
var ttsQuantSuffixes = []string{"-Q4_K_M", "-Q8_0", "-Q4_0", "-bf16", "-F16"}

// ttsModelFiles returns the backbone and mmproj GGUF filenames for a model id.
// Accepted values: "" (or "base", the default Q4_K_M preset), "q8", "bf16", or
// explicit "<backbone>.gguf" / "<backbone>.gguf,<mmproj>.gguf" filenames from
// the same repository.
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

// TTSModelManager resolves and (optionally) downloads the TTS GGUF models
// (backbone + mmproj) into the models dir, mirroring ModelManager for whisper.
type TTSModelManager struct {
	cfg config.TextToSpeechConfig

	// baseURL and client are overridable in tests.
	baseURL string
	client  *http.Client
}

// NewTTSModelManager creates a TTSModelManager from the text-to-speech config.
func NewTTSModelManager(cfg config.TextToSpeechConfig) *TTSModelManager {
	return &TTSModelManager{
		cfg:     cfg,
		baseURL: qwen3TTSBase,
		client:  http.DefaultClient,
	}
}

// modelsDir returns the directory holding TTS models, defaulting to
// ~/.infer/models/tts when not configured.
func (m *TTSModelManager) modelsDir() (string, error) {
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
// downloading them on first use when AutoDownload is enabled. Downloads are
// cached; existing files are returned as-is.
func (m *TTSModelManager) EnsureModels(ctx context.Context) (backbone, mmproj string, err error) {
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
func (m *TTSModelManager) ensureFile(ctx context.Context, name string) (string, error) {
	dir, err := m.modelsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if !m.cfg.AutoDownload {
		return "", fmt.Errorf("tts model %q not found at %s and text_to_speech.auto_download is disabled", name, path)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating models directory: %w", err)
	}

	if err := m.download(ctx, m.baseURL+"/"+name, path); err != nil {
		return "", err
	}
	return path, nil
}

// download fetches url into dstPath atomically (temp file + rename) so an
// interrupted download never leaves a half-written model behind.
func (m *TTSModelManager) download(ctx context.Context, url, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading tts model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading tts model: status %d from %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".tts-*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing tts model: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing tts model: %w", err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing tts model: %w", err)
	}
	return nil
}
