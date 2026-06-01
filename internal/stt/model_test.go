package stt

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestModelFileName(t *testing.T) {
	cases := map[string]string{
		"":               "ggml-tiny.bin",
		"tiny":           "ggml-tiny.bin",
		"base.en":        "ggml-base.en.bin",
		"small":          "ggml-small.bin",
		"  medium  ":     "ggml-medium.bin",
		"ggml-large.bin": "ggml-large.bin",
		"custom.bin":     "custom.bin",
	}
	for in, want := range cases {
		if got := modelFileName(in); got != want {
			t.Errorf("modelFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEnsureModelCached(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "ggml-tiny.bin")
	if err := os.WriteFile(modelPath, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModelManager(config.SpeechToTextConfig{Model: "tiny", ModelsDir: dir, AutoDownload: false})
	got, err := m.EnsureModel(context.Background())
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	if got != modelPath {
		t.Errorf("EnsureModel = %q, want %q", got, modelPath)
	}
}

func TestEnsureModelMissingNoDownload(t *testing.T) {
	dir := t.TempDir()
	m := NewModelManager(config.SpeechToTextConfig{Model: "tiny", ModelsDir: dir, AutoDownload: false})
	if _, err := m.EnsureModel(context.Background()); err == nil {
		t.Fatal("expected error when model is missing and auto_download is disabled")
	}
}

func TestEnsureModelDownloads(t *testing.T) {
	const body = "ggml-model-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ggml-tiny.bin" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewModelManager(config.SpeechToTextConfig{Model: "tiny", ModelsDir: dir, AutoDownload: true})
	m.baseURL = srv.URL
	m.client = srv.Client()

	got, err := m.EnsureModel(context.Background())
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("downloaded content = %q, want %q", data, body)
	}

	// Second call should hit the cache (server would 404 a different path anyway).
	if _, err := m.EnsureModel(context.Background()); err != nil {
		t.Fatalf("cached EnsureModel: %v", err)
	}
}

func TestEnsureModelDownloadBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewModelManager(config.SpeechToTextConfig{Model: "tiny", ModelsDir: dir, AutoDownload: true})
	m.baseURL = srv.URL
	m.client = srv.Client()

	if _, err := m.EnsureModel(context.Background()); err == nil {
		t.Fatal("expected error on non-200 download status")
	}
	// Failed download must not leave a partial model behind.
	if _, err := os.Stat(filepath.Join(dir, "ggml-tiny.bin")); !os.IsNotExist(err) {
		t.Errorf("expected no model file after failed download, stat err = %v", err)
	}
}
