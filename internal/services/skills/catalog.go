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
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const (
	catalogFile         = "catalog.json"
	catalogTimeout      = 15 * time.Second
	catalogIndexTimeout = 3 * time.Second
	catalogUA           = "inference-gateway-cli"
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
	client     *http.Client
	baseURL    string
	repository string

	mu     sync.Mutex
	index  []catalogEntry
	cached bool
}

// NewCatalogClient returns a CatalogClient pointed at the configured registry
// URL. When registry_url is unset the index base is derived from the configured
// skills repository, so agent.skills.repository alone redirects both the index
// and the downloads; an explicit registry_url still wins (a catalog served from
// somewhere other than the repo itself).
func NewCatalogClient(cfg *config.Config) *CatalogClient {
	repository := cfg.Agent.Skills.SkillsRepository()

	baseURL := cfg.Agent.Skills.Discovery.RegistryURL
	if baseURL == "" {
		baseURL = fmt.Sprintf("%s/%s/%s", githubRawBase, repository, defaultSkillsRef)
	}
	baseURL = strings.TrimRight(baseURL, "/") + "/"

	return &CatalogClient{
		client:     &http.Client{Timeout: catalogTimeout},
		baseURL:    baseURL,
		repository: repository,
	}
}

// Index fetches the catalog index (name + description for every published
// skill) and caches it for the process lifetime. Returns nil on any failure -
// the catalog is best-effort, never fatal. No body download.
func (c *CatalogClient) Index(ctx context.Context) []catalogEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached {
		return c.index
	}

	indexURL, err := url.JoinPath(c.baseURL, catalogFile)
	if err != nil {
		logger.Debug("failed to build catalog index URL", "error", err)
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", indexURL, nil)
	if err != nil {
		logger.Debug("failed to create catalog request", "error", err)
		return nil
	}
	req.Header.Set("User-Agent", catalogUA)
	req.Header.Set("Accept", "application/json")
	if token := githubToken(); token != "" && isGitHubHost(req.URL.Host) {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		logger.Debug("failed to fetch catalog index", "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("catalog index returned non-200", "status", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Debug("failed to read catalog index body", "error", err)
		return nil
	}

	var catalog catalogResponse
	if err := json.Unmarshal(body, &catalog); err != nil {
		logger.Debug("failed to parse catalog index", "error", err)
		return nil
	}

	c.index = catalog.Skills
	c.cached = true
	return c.index
}

// isGitHubHost reports whether host is github.com or one of its content
// subdomains, so credentials stay scoped to the vendor that issued them.
func isGitHubHost(host string) bool {
	host = strings.ToLower(host)
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	for _, suffix := range []string{"github.com", "githubusercontent.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// Lookup queries the catalog index for a skill with the given name.
// Returns the entry metadata and true on success, or false when the
// skill is not found.
func (c *CatalogClient) Lookup(ctx context.Context, name string) (*catalogEntry, bool) {
	for _, entry := range c.Index(ctx) {
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

	installer := NewInstaller(c.repository)
	absPath, err := installer.InstallFromGitHub(ctx, SkillTreeURL(c.repository, name), destBase, false)
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
