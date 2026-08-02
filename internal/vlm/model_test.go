package vlm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestEnsureModelDownloadsPair(t *testing.T) {
	var requested []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		_, _ = w.Write([]byte("gguf-bytes"))
	}))
	defer ts.Close()

	dir := t.TempDir()
	m := NewModelManager(config.VisionAnnotatorConfig{Model: "qwen3-vl-2b", ModelsDir: dir, AutoDownload: true})
	m.baseURL = ts.URL

	llm, mmproj, err := m.EnsureModel(context.Background())
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	if len(requested) != 2 {
		t.Fatalf("expected 2 downloads, got %v", requested)
	}
	for _, p := range []string{llm, mmproj} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected downloaded file at %s: %v", p, err)
		}
	}

	// second call: cached, no new downloads
	requested = nil
	if _, _, err := m.EnsureModel(context.Background()); err != nil {
		t.Fatalf("EnsureModel (cached): %v", err)
	}
	if len(requested) != 0 {
		t.Errorf("expected no downloads on cached call, got %v", requested)
	}
}

func TestEnsureModelAutoDownloadDisabled(t *testing.T) {
	m := NewModelManager(config.VisionAnnotatorConfig{Model: "qwen3-vl-2b", ModelsDir: t.TempDir(), AutoDownload: false})
	_, _, err := m.EnsureModel(context.Background())
	if err == nil || !strings.Contains(err.Error(), "auto_download is disabled") {
		t.Fatalf("expected auto_download error, got %v", err)
	}
}

func TestEnsureModelUnknownModel(t *testing.T) {
	m := NewModelManager(config.VisionAnnotatorConfig{Model: "moondream-99b", ModelsDir: t.TempDir(), AutoDownload: true})
	_, _, err := m.EnsureModel(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown local vision model") {
		t.Fatalf("expected unknown-model error, got %v", err)
	}
}

func TestEnsureModelPartialPairDownloadsMissing(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Qwen3VL-2B-Instruct-Q4_K_M.gguf"), []byte("m"), 0o644); err != nil {
		t.Fatal(err)
	}

	var requested []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		_, _ = w.Write([]byte("gguf-bytes"))
	}))
	defer ts.Close()

	m := NewModelManager(config.VisionAnnotatorConfig{Model: "qwen3-vl-2b", ModelsDir: dir, AutoDownload: true})
	m.baseURL = ts.URL

	if _, _, err := m.EnsureModel(context.Background()); err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	if len(requested) != 1 || !strings.Contains(requested[0], "mmproj") {
		t.Errorf("expected only the mmproj download, got %v", requested)
	}
}
