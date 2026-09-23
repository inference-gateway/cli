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

// whisperRepo hosts the ggml whisper.cpp models, stored as ggml-<name>.bin.
var whisperRepo = huggingface.Repo{ID: "ggerganov/whisper.cpp"}

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
	mu  sync.Mutex

	// hub is overridable in tests.
	hub *huggingface.Client
}

// NewModelManager creates a ModelManager from the speech-to-text config.
func NewModelManager(cfg config.SpeechToTextConfig) *ModelManager {
	return &ModelManager{
		cfg: cfg,
		hub: huggingface.NewClient(),
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

// EnsureModel returns the local path to the model file, downloading it on first
// use when AutoDownload is enabled. Concurrent callers are serialized so a
// cold cache triggers one download.
func (m *ModelManager) EnsureModel(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	dir, err := m.modelsDir()
	if err != nil {
		return "", err
	}
	name := modelFileName(m.cfg.Model)

	path, err := m.hub.EnsureFile(ctx, whisperRepo, name, dir, "whisper model", m.cfg.AutoDownload, agentdomain.GetToolProgressCallback(ctx))
	if errors.Is(err, huggingface.ErrNotCached) {
		return "", fmt.Errorf("whisper model %q not found at %s and speech_to_text.auto_download is disabled", m.cfg.Model, filepath.Join(dir, name))
	}
	return path, err
}
