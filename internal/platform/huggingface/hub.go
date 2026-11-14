package huggingface

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	searchSort      = "downloads"
	searchDirection = "-1"
	treeFileType    = "file"
	userAgent       = "inference-gateway-cli"
)

// Model is a model repository as the Hub's search lists it.
type Model struct {
	ID          string `json:"id"`
	Downloads   int64  `json:"downloads"`
	Likes       int64  `json:"likes"`
	PipelineTag string `json:"pipeline_tag,omitempty"`
}

// File is one file of a repository, with its size in bytes.
type File struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Search returns up to limit model repositories matching query, the most
// downloaded first.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Model, error) {
	params := url.Values{}
	params.Set("search", query)
	params.Set("limit", strconv.Itoa(limit))
	params.Set("sort", searchSort)
	params.Set("direction", searchDirection)

	var found []Model
	if err := c.getJSON(ctx, c.BaseURL+"/api/models?"+params.Encode(), &found); err != nil {
		return nil, fmt.Errorf("searching the hub for %q: %w", query, err)
	}
	return found, nil
}

// Files lists the files of repo at its revision, folders left out.
// shortcut: reads the first page the Hub answers, follow its Link header once a repository outgrows it.
func (c *Client) Files(ctx context.Context, repo Repo) ([]File, error) {
	revision := repo.Revision
	if revision == "" {
		revision = defaultRevision
	}
	endpoint := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", c.BaseURL, repo.ID, url.PathEscape(revision))

	var entries []struct {
		Type string `json:"type"`
		File
	}
	if err := c.getJSON(ctx, endpoint, &entries); err != nil {
		return nil, fmt.Errorf("listing the files of %s: %w", repo.ID, err)
	}

	files := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.Type == treeFileType {
			files = append(files, entry.File)
		}
	}
	return files, nil
}

func (c *Client) getJSON(ctx context.Context, endpoint string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d from %s", resp.StatusCode, endpoint)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
