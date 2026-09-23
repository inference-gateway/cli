package huggingface

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	download "github.com/inference-gateway/cli/internal/platform/download"
)

// DefaultBaseURL is the public Hugging Face Hub.
const DefaultBaseURL = "https://huggingface.co"

// defaultRevision is the revision a Repo resolves to when it names none.
const defaultRevision = "main"

// ErrNotCached reports a file that is missing from the cache while the caller
// did not allow a download. Callers wrap it with the config key that gates it.
var ErrNotCached = errors.New("file not cached and download not allowed")

// Repo identifies a model repository on the Hub, e.g. "ggerganov/whisper.cpp".
// An empty Revision means "main"; set a commit SHA to pin the files.
type Repo struct {
	ID       string
	Revision string
}

// Client downloads repository files. BaseURL and HTTP are overridable in tests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient returns a Client for the public Hub.
func NewClient() *Client {
	return &Client{BaseURL: DefaultBaseURL, HTTP: http.DefaultClient}
}

// fileURL returns the Hub's resolve URL for file in repo.
func (c *Client) fileURL(repo Repo, file string) string {
	revision := repo.Revision
	if revision == "" {
		revision = defaultRevision
	}
	return fmt.Sprintf("%s/%s/resolve/%s/%s", c.BaseURL, repo.ID, revision, file)
}

// EnsureFile returns the local path of file inside dir, downloading it from
// repo when it is missing and allowDownload is true. A missing file with
// downloads disallowed yields ErrNotCached. label names the file in progress
// reports (sent to report, nil for none) and errors (e.g. "whisper model").
func (c *Client) EnsureFile(ctx context.Context, repo Repo, file, dir, label string, allowDownload bool, report func(string)) (string, error) {
	path := filepath.Join(dir, file)

	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	if !allowDownload {
		return "", ErrNotCached
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating models directory: %w", err)
	}

	if err := download.ToFile(ctx, c.HTTP, c.fileURL(repo, file), path, label, report); err != nil {
		return "", err
	}
	return path, nil
}
