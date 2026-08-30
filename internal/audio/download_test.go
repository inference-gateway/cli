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

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
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

func TestProgressReaderMessage(t *testing.T) {
	tests := []struct {
		name  string
		total int64
		read  int64
		want  string
	}{
		{"known size", 8 << 20, 2 << 20, "Downloading tts model: 2 MB of 8 MB (25%)"},
		{"unknown size", 0, 3 << 20, "Downloading tts model: 3 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &progressReader{label: "tts model", total: tt.total, read: tt.read}
			if got := p.message(); got != tt.want {
				t.Errorf("message() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Without a callback in the context the source reader must pass through
// untouched, so headless runs and tests pay nothing for the instrumentation.
func TestNewProgressReaderWithoutCallbackPassesThrough(t *testing.T) {
	src := strings.NewReader("data")
	if got := newProgressReader(context.Background(), src, "tts model", 4); got != io.Reader(src) {
		t.Error("expected the source reader unwrapped when no progress callback is set")
	}
}

func TestDownloadToFileReportsProgress(t *testing.T) {
	body := strings.Repeat("x", 1<<20)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	var reports []string
	ctx := agentdomain.WithToolProgressCallback(context.Background(), func(message string) {
		reports = append(reports, message)
	})

	dst := filepath.Join(t.TempDir(), "model.gguf")
	if err := downloadToFile(ctx, srv.Client(), srv.URL+"/model.gguf", dst, "tts model"); err != nil {
		t.Fatalf("downloadToFile: %v", err)
	}

	if len(reports) == 0 {
		t.Fatal("expected at least one progress report")
	}
	if !strings.HasPrefix(reports[0], "Downloading tts model: ") {
		t.Errorf("first report = %q, want a \"Downloading tts model: \" prefix", reports[0])
	}
}
