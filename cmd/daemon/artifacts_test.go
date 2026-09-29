package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestArtifactsHandlerServesAFile(t *testing.T) {
	projects := t.TempDir()
	dir := filepath.Join(projects, "-home-alice-repo", config.ArtifactsDirName, "sess-1")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "image-1.png")
	if err := os.WriteFile(file, []byte("png-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	newTestArtifactsHandler(projects).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/artifacts/-home-alice-repo/sess-1/image-1.png", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "png-bytes" {
		t.Fatalf("body = %q, want the file's bytes", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("content-type = %q, want image/png", ct)
	}
}

// TestArtifactsHandlerRejectsTraversal covers a path that would climb out of
// the artifacts subtree and requests whose slug is not a local single segment.
func TestArtifactsHandlerRejectsTraversal(t *testing.T) {
	projects := t.TempDir()
	artifacts := filepath.Join(projects, "-home-alice-repo", config.ArtifactsDirName)
	for _, name := range []string{
		filepath.Join(projects, "outside.txt"),
		filepath.Join(projects, "-home-alice-repo", "outside.txt"),
		filepath.Join(artifacts, "..", "..", "escape.txt"),
	} {
		if err := os.MkdirAll(filepath.Dir(name), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("secret-bytes"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name string
		path string
		want int
	}{
		{"relative climb through the artifacts dir", "/artifacts/-home-alice-repo/../escape.txt", http.StatusNotFound},
		{"double climb out of the artifacts dir", "/artifacts/-home-alice-repo/../../escape.txt", http.StatusNotFound},
		{"climb out of the projects root", "/artifacts/-home-alice-repo/sess/../../outside.txt", http.StatusNotFound},
		{"slug above the projects root", "/artifacts/../escape.txt", http.StatusNotFound},
		{"slug is not a single segment", "/artifacts/-home-alice/other/outside.txt", http.StatusNotFound},
		{"missing file", "/artifacts/-home-alice-repo/sess-1/absent.png", http.StatusNotFound},
		{"directory", "/artifacts/-home-alice-repo/sess-1", http.StatusNotFound},
		{"no slug", "/artifacts/", http.StatusNotFound},
		{"root path", "/artifacts", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			newTestArtifactsHandler(projects).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if strings.Contains(rec.Body.String(), "secret-bytes") {
				t.Fatalf("status %d leaked out-of-tree bytes", rec.Code)
			}
		})
	}

	rec := httptest.NewRecorder()
	newTestArtifactsHandler(projects).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/artifacts/-home-alice-repo/anything", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status = %d, want 405", rec.Code)
	}
}

// newTestArtifactsHandler is the handler pinned to a temp projects root.
func newTestArtifactsHandler(projects string) *artifactsHandler {
	return &artifactsHandler{projectsDir: projects}
}
