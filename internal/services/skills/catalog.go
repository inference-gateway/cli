package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	defaultRegistryURL = "https://registry.inference-gateway.com/skills/"
	catalogTimeout     = 15 * time.Second
	catalogUA          = "inference-gateway-cli"
)

// catalogEntry is the metadata for one skill in the centralized catalog.
// Only name and description are consulted up front; the body is fetched
// on demand when the skill is activated (progressive disclosure).
type catalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// catalogResponse is the top-level response from the catalog index.
type catalogResponse struct {
	Skills []catalogEntry `json:"skills"`
}

// CatalogClient fetches skill metadata from the centralized skills registry.
// It implements progressive discovery: only the index (name + description)
// is fetched up front; the full skill body is downloaded only on activation.
type CatalogClient struct {
	client  *http.Client
	baseURL string
}

// NewCatalogClient returns a CatalogClient pointed at the configured registry
// URL, or the default when none is set.
func NewCatalogClient(cfg *config.Config) *CatalogClient {
	baseURL := cfg.Agent.Skills.Discovery.RegistryURL
	if baseURL == "" {
		baseURL = defaultRegistryURL
	}
	baseURL = strings.TrimRight(baseURL, "/") + "/"

	return &CatalogClient{
		client:  &http.Client{Timeout: catalogTimeout},
		baseURL: baseURL,
	}
}

// Lookup queries the catalog index for a skill with the given name.
// Returns the entry metadata and true on success, or false when the
// skill is not found. Only the index is consulted - no body download.
func (c *CatalogClient) Lookup(ctx context.Context, name string) (*catalogEntry, bool) {
	indexURL, err := url.JoinPath(c.baseURL, "index.json")
	if err != nil {
		logger.Debug("failed to build catalog index URL", "error", err)
		return nil, false
	}

	req, err := http.NewRequestWithContext(ctx, "GET", indexURL, nil)
	if err != nil {
		logger.Debug("failed to create catalog request", "error", err)
		return nil, false
	}
	req.Header.Set("User-Agent", catalogUA)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Debug("failed to fetch catalog index", "error", err)
		return nil, false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("catalog index returned non-200", "status", resp.StatusCode)
		return nil, false
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug("failed to read catalog index body", "error", err)
		return nil, false
	}

	var catalog catalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		logger.Debug("failed to parse catalog index", "error", err)
		return nil, false
	}

	for _, entry := range catalog.Skills {
		if entry.Name == name {
			return &entry, true
		}
	}

	return nil, false
}

// dynamicSkillsDir returns the directory where dynamically downloaded skills
// are stored. It lives under the project's .infer/tmp/skills/ so it is
// ephemeral and cleaned up after the session.
func dynamicSkillsDir() (string, error) {
	// Use the project tmp dir for dynamic skills so they are scoped to
	// the current session and cleaned up automatically.
	base := filepath.Join(config.ConfigDirName, "tmp", "skills")
	abs, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("failed to resolve dynamic skills dir: %w", err)
	}
	return abs, nil
}

// DownloadSkill fetches the full skill folder from the catalog and writes it
// to the dynamic skills directory. Returns the path to the downloaded SKILL.md.
func (c *CatalogClient) DownloadSkill(ctx context.Context, name string) (string, error) {
	destBase, err := dynamicSkillsDir()
	if err != nil {
		return "", err
	}

	// Reuse the existing Installer to download the skill from GitHub.
	// The catalog skills live in inference-gateway/skills on the main branch.
	installer := NewInstaller()
	rawURL := fmt.Sprintf("https://github.com/%s/%s/tree/%s/%s/%s",
		defaultSkillsOrg, defaultSkillsRepo, defaultSkillsRef, defaultSkillsSubdir, name)

	absPath, err := installer.InstallFromGitHub(ctx, rawURL, destBase, false)
	if err != nil {
		return "", fmt.Errorf("failed to download skill %q from catalog: %w", name, err)
	}

	return filepath.Join(absPath, skillEntryFile), nil
}

// CleanupDynamic removes all dynamically downloaded skills from the
// temporary skills directory. Called after the session ends.
func CleanupDynamic() error {
	dir, err := dynamicSkillsDir()
	if err != nil {
		return err
	}

	if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
		return nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read dynamic skills dir: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(dir, entry.Name())
		if rmErr := os.RemoveAll(skillDir); rmErr != nil {
			logger.Warn("failed to remove dynamic skill", "path", skillDir, "error", rmErr)
		}
	}

	return nil
}
