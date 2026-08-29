package audio

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stubTransport replies with a canned response for every request.
type stubTransport func(*http.Request) (*http.Response, error)

func (s stubTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return s(r)
}

func TestDownloadToFileWritesContent(t *testing.T) {
	const body = "model-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	dir := t.TempDir()
	dst := filepath.Join(dir, "model.gguf")

	if err := downloadToFile(context.Background(), srv.Client(), srv.URL+"/model.gguf", dst, "tts model"); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body {
		t.Errorf("content = %q, want %q", data, body)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected only the final model file, got %d entries", len(entries))
	}
}

func TestDownloadToFileRejectsIncompleteDownload(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "model.gguf")

	// A body shorter than the advertised Content-Length must be rejected,
	// never cached, and leave no temp file behind.
	c := &http.Client{Transport: stubTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: 10,
			Header:        http.Header{},
			Body:          io.NopCloser(strings.NewReader("short")),
		}, nil
	})}
	if err := downloadToFile(context.Background(), c, "http://x/model.gguf", dst, "tts model"); err == nil {
		t.Fatal("expected error when fewer bytes than Content-Length arrive")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("incomplete download must not reach the cache, stat err = %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("leftover files after failed download: %d", len(entries))
	}
}

func TestCachedModelStale(t *testing.T) {
	const remoteBody = "server-copy-bytes"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(remoteBody)))
	}))
	defer srv.Close()

	offline := &http.Client{Transport: stubTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}

	tests := []struct {
		name      string
		autoDL    bool
		file      bool
		content   string
		client    *http.Client
		wantStale bool
	}{
		{"manual models are never re-fetched", false, true, "trunc", srv.Client(), false},
		{"missing file is not stale", true, false, "", srv.Client(), false},
		{"size matching server is fresh", true, true, remoteBody, srv.Client(), false},
		{"truncated cache is stale", true, true, "trunc", srv.Client(), true},
		{"probe outage trusts cache", true, true, "whatever", offline, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "m.gguf")
			if tt.file {
				if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			got := cachedModelStale(context.Background(), tt.client, srv.URL+"/m.gguf", path, tt.autoDL)
			if got != tt.wantStale {
				t.Errorf("cachedModelStale(autoDL=%v, file=%v, content=%q) = %v, want %v",
					tt.autoDL, tt.file, tt.content, got, tt.wantStale)
			}
		})
	}
}
