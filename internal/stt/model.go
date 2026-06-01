package stt

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

// huggingFaceWhisperBase is the resolve base for ggml whisper.cpp models.
// Files are downloaded as <base>/ggml-<name>.bin.
const huggingFaceWhisperBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

// modelFileName returns the ggml filename for a model name. It accepts short
// names ("tiny", "base.en"), already-prefixed names ("ggml-tiny.bin"), or any
// "*.bin" filename. An empty model defaults to "tiny".
func modelFileName(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		m = "tiny"
	}
	if strings.HasSuffix(m, ".bin") {
		return m
	}
	return "ggml-" + m + ".bin"
}

// ModelManager resolves and (optionally) downloads the GGML model file.
type ModelManager struct {
	cfg config.SpeechToTextConfig

	// baseURL and client are overridable in tests.
	baseURL string
	client  *http.Client
}

// NewModelManager creates a ModelManager from the speech-to-text config.
func NewModelManager(cfg config.SpeechToTextConfig) *ModelManager {
	return &ModelManager{
		cfg:     cfg,
		baseURL: huggingFaceWhisperBase,
		client:  http.DefaultClient,
	}
}

// modelsDir returns the directory holding whisper models, defaulting to
// ~/.infer/models/whisper when not configured.
func (m *ModelManager) modelsDir() (string, error) {
	if strings.TrimSpace(m.cfg.ModelsDir) != "" {
		return m.cfg.ModelsDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "models", "whisper"), nil
}

// modelURL returns the download URL for the configured model.
func (m *ModelManager) modelURL() string {
	return m.baseURL + "/" + modelFileName(m.cfg.Model)
}

// EnsureModel returns the local path to the model file, downloading it on first
// use when AutoDownload is enabled. Downloads are cached; an existing file is
// returned as-is.
func (m *ModelManager) EnsureModel(ctx context.Context) (string, error) {
	dir, err := m.modelsDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, modelFileName(m.cfg.Model))

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if !m.cfg.AutoDownload {
		return "", fmt.Errorf("whisper model %q not found at %s and speech_to_text.auto_download is disabled", m.cfg.Model, path)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating models directory: %w", err)
	}

	if err := m.download(ctx, m.modelURL(), path); err != nil {
		return "", err
	}
	return path, nil
}

// download fetches url into dstPath atomically (download to a temp file, then
// rename) so an interrupted download never leaves a half-written model behind.
func (m *ModelManager) download(ctx context.Context, url, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading whisper model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading whisper model: status %d from %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".ggml-*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing whisper model: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing whisper model: %w", err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing whisper model: %w", err)
	}
	return nil
}
