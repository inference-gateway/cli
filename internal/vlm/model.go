package vlm

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

// huggingFaceBase is the resolve base for GGUF model files.
const huggingFaceBase = "https://huggingface.co"

// modelSpec names the GGUF pair (language model + vision projector) of a
// short model name.
type modelSpec struct {
	repo   string
	llm    string
	mmproj string
}

// localModels maps vision.annotator.model short names to their HF GGUF pair.
var localModels = map[string]modelSpec{
	"qwen3-vl-2b": {
		repo:   "Qwen/Qwen3-VL-2B-Instruct-GGUF",
		llm:    "Qwen3VL-2B-Instruct-Q4_K_M.gguf",
		mmproj: "mmproj-Qwen3VL-2B-Instruct-F16.gguf",
	},
	"qwen3-vl-4b": {
		repo:   "Qwen/Qwen3-VL-4B-Instruct-GGUF",
		llm:    "Qwen3VL-4B-Instruct-Q4_K_M.gguf",
		mmproj: "mmproj-Qwen3VL-4B-Instruct-F16.gguf",
	},
}

// ModelManager resolves and (optionally) downloads the GGUF model pair,
// mirroring stt.ModelManager but pair-aware (LLM + mmproj).
type ModelManager struct {
	cfg config.VisionAnnotatorConfig

	// baseURL and client are overridable in tests.
	baseURL string
	client  *http.Client
}

// NewModelManager creates a ModelManager from the vision annotator config.
func NewModelManager(cfg config.VisionAnnotatorConfig) *ModelManager {
	return &ModelManager{
		cfg:     cfg,
		baseURL: huggingFaceBase,
		client:  http.DefaultClient,
	}
}

// modelsDir returns the directory holding VLM models, defaulting to
// ~/.infer/models/vlm when not configured.
func (m *ModelManager) modelsDir() (string, error) {
	if strings.TrimSpace(m.cfg.ModelsDir) != "" {
		return m.cfg.ModelsDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "models", "vlm"), nil
}

// EnsureModel returns the local paths to the model pair, downloading them on
// first use when AutoDownload is enabled. Existing files are returned as-is.
func (m *ModelManager) EnsureModel(ctx context.Context) (llmPath, mmprojPath string, err error) {
	spec, ok := localModels[strings.TrimSpace(m.cfg.Model)]
	if !ok {
		names := make([]string, 0, len(localModels))
		for name := range localModels {
			names = append(names, name)
		}
		return "", "", fmt.Errorf("unknown local vision model %q: known models are %s (or use engine \"gateway\" with a provider/model)",
			m.cfg.Model, strings.Join(names, ", "))
	}

	dir, err := m.modelsDir()
	if err != nil {
		return "", "", err
	}

	llmPath = filepath.Join(dir, spec.llm)
	mmprojPath = filepath.Join(dir, spec.mmproj)
	for _, f := range []string{spec.llm, spec.mmproj} {
		path := filepath.Join(dir, f)
		if _, statErr := os.Stat(path); statErr == nil {
			continue
		}
		if !m.cfg.AutoDownload {
			return "", "", fmt.Errorf("vision model file %q not found at %s and vision.annotator.auto_download is disabled", f, path)
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "", fmt.Errorf("creating models directory: %w", err)
		}
		url := fmt.Sprintf("%s/%s/resolve/main/%s", m.baseURL, spec.repo, f)
		if err := m.download(ctx, url, path); err != nil {
			return "", "", err
		}
	}
	return llmPath, mmprojPath, nil
}

// download fetches url into dstPath atomically (download to a temp file, then
// rename) so an interrupted download never leaves a half-written model behind.
// ponytail: third copy of the atomic-download loop (stt has two callers of
// one); extract a shared helper if a fourth appears.
func (m *ModelManager) download(ctx context.Context, url, dstPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading vision model: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading vision model: status %d from %s", resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), ".gguf-*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing vision model: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing vision model: %w", err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing vision model: %w", err)
	}
	return nil
}
