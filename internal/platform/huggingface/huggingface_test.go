package huggingface

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestFileURL(t *testing.T) {
	tests := []struct {
		name string
		repo Repo
		want string
	}{
		{"empty revision resolves to main", Repo{ID: "ggerganov/whisper.cpp"}, "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin"},
		{"pinned revision", Repo{ID: "ggerganov/whisper.cpp", Revision: "abc123"}, "https://huggingface.co/ggerganov/whisper.cpp/resolve/abc123/ggml-tiny.bin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewClient().fileURL(tt.repo, "ggml-tiny.bin"); got != tt.want {
				t.Errorf("fileURL = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnsureFileCachedMakesNoRequest(t *testing.T) {
	var gets atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		gets.Add(1)
	}))
	defer srv.Close()

	dir := t.TempDir()
	want := filepath.Join(dir, "model.bin")
	if err := os.WriteFile(want, []byte("cached"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.EnsureFile(context.Background(), Repo{ID: "org/repo"}, "model.bin", dir, "model", true, nil)
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if got != want {
		t.Errorf("EnsureFile = %q, want %q", got, want)
	}
	if n := gets.Load(); n != 0 {
		t.Errorf("requests = %d, want none for a cached file", n)
	}
}

func TestEnsureFileMissingWithoutDownload(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "models")
	_, err := NewClient().EnsureFile(context.Background(), Repo{ID: "org/repo"}, "model.bin", dir, "model", false, nil)
	if !errors.Is(err, ErrNotCached) {
		t.Fatalf("err = %v, want ErrNotCached", err)
	}
	if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
		t.Errorf("cache dir must not be created when downloads are disallowed, stat err = %v", statErr)
	}
}

func TestEnsureFileDownloads(t *testing.T) {
	const body = "model-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/org/repo/resolve/main/model.bin" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "models")
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	got, err := c.EnsureFile(context.Background(), Repo{ID: "org/repo"}, "model.bin", dir, "model", true, nil)
	if err != nil {
		t.Fatalf("EnsureFile: %v", err)
	}
	if got != filepath.Join(dir, "model.bin") {
		t.Errorf("EnsureFile = %q, want the file inside %q", got, dir)
	}
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("content = %q, want %q", data, body)
	}
}

func TestEnsureFileBadStatusLeavesNothingBehind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.EnsureFile(context.Background(), Repo{ID: "org/repo"}, "model.bin", dir, "model", true, nil); err == nil {
		t.Fatal("expected error on non-200 download status")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover files after failed download: %d", len(entries))
	}
}
