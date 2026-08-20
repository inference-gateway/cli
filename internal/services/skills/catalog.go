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

	fuzzy "github.com/sahilm/fuzzy"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	catalogFile             = "catalog.json"
	catalogTimeout          = 15 * time.Second
	catalogIndexTimeout     = 3 * time.Second
	catalogUA               = "inference-gateway-cli"
	catalogMaxBytes         = 8 << 20
	catalogSearchMaxResults = 200
)

// catalogEntry is the metadata for one skill in the centralized catalog.
// Only name and description are consulted up front; the body is fetched
// on demand when the skill is activated (progressive disclosure).
type catalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
}

// catalogResponse is the top-level response from the catalog index. Release
// and Updated describe the catalog as a whole - individual entries carry no
// version of their own.
type catalogResponse struct {
	Release string         `json:"release"`
	Updated string         `json:"updated"`
	Skills  []catalogEntry `json:"skills"`
}

// CatalogClient fetches skill metadata from the centralized skills registry.
// It implements progressive discovery: only the index (name + description)
// is fetched up front; the full skill body is downloaded only on activation.
type CatalogClient struct {
	client     *http.Client
	baseURL    string
	repository string

	mu      sync.Mutex
	index   []catalogEntry
	release string
	updated string
	cached  bool
}

// NewCatalogClient returns a CatalogClient for the configured skills
// repository. Both the index and the skill bodies come from that one repo, so
// agent.skills.repository is the only knob needed to point the CLI at a fork.
func NewCatalogClient(cfg *config.Config) *CatalogClient {
	repository := cfg.Agent.Skills.SkillsRepository()
	return newCatalogClientWithBase(
		fmt.Sprintf("%s/%s/%s", githubRawBase, repository, defaultSkillsRef),
		repository,
	)
}

// newCatalogClientWithBase points the index at an arbitrary base URL. Only
// tests need this: downloads still resolve against repository, so a base
// pointing somewhere else can only serve an index for skills that live in that
// repo anyway.
func newCatalogClientWithBase(baseURL, repository string) *CatalogClient {
	return &CatalogClient{
		client:     &http.Client{Timeout: catalogTimeout},
		baseURL:    strings.TrimRight(baseURL, "/") + "/",
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes))
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
	c.release = catalog.Release
	c.updated = catalog.Updated
	c.cached = true
	return c.index
}

// Release returns the catalog's published release version and update timestamp
// as of the last Index fetch. Both are empty when the catalog has not been
// fetched or does not publish them. Individual skills carry no version of their
// own - the catalog is versioned as a whole.
func (c *CatalogClient) Release() (release, updated string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.release, c.updated
}

// skillFromEntry converts a catalog index entry into a metadata-only skill
// (empty Path = body not downloaded yet). The description is truncated to the
// same limit local skills are validated against: catalog entries are remote
// input that skips that validation, and the string ends up in the system
// prompt.
func skillFromEntry(entry catalogEntry) domain.Skill {
	desc := entry.Description
	if len(desc) > skillDescMaxLen {
		desc = strings.ToValidUTF8(desc[:skillDescMaxLen], "")
	}
	return domain.Skill{
		Name:        entry.Name,
		Description: desc,
		Scope:       domain.SkillScopeCatalog,
	}
}

// Search matches query against the catalog, best match first, capped at limit.
// An empty query returns the head of the index so the command doubles as a
// browse.
//
// Ranking, in order: names fuzzy-matched by score, then entries whose
// description literally contains the query. Fuzzy runs over names ONLY -
// a subsequence match against a paragraph of prose is meaningless ("rust"
// matches r..u..s..t in most English sentences), which makes description
// matching substring-only.
//
// Matching is local over the cached index because catalog.json is a single
// static file with no server-side query support - see NewCatalogClient.
func (c *CatalogClient) Search(ctx context.Context, query string, limit int) []domain.Skill {
	entries := c.Index(ctx)
	if limit <= 0 || limit > catalogSearchMaxResults {
		limit = catalogSearchMaxResults
	}

	if strings.TrimSpace(query) == "" {
		if len(entries) > limit {
			entries = entries[:limit]
		}
		out := make([]domain.Skill, 0, len(entries))
		for _, entry := range entries {
			out = append(out, skillFromEntry(entry))
		}
		return out
	}

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name
	}

	out := make([]domain.Skill, 0, limit)
	taken := make(map[int]struct{}, limit)
	add := func(i int) {
		if _, dup := taken[i]; dup {
			return
		}
		taken[i] = struct{}{}
		out = append(out, skillFromEntry(entries[i]))
	}

	for _, match := range fuzzy.Find(query, names) {
		if len(out) >= limit {
			return out
		}
		add(match.Index)
	}

	needle := strings.ToLower(strings.TrimSpace(query))
	for i, entry := range entries {
		if len(out) >= limit {
			break
		}
		if containsWord(strings.ToLower(entry.Description), needle) {
			add(i)
		}
	}

	return out
}

// containsWord reports whether needle occurs in haystack delimited by
// non-word characters. Both must already be lowercased.
//
// A plain substring match makes short queries useless: "go" hits "good",
// "going" and "algorithm" across half the catalog.
func containsWord(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for offset := 0; offset+len(needle) <= len(haystack); {
		i := strings.Index(haystack[offset:], needle)
		if i < 0 {
			return false
		}
		start := offset + i
		if !isWordByte(haystack, start-1) && !isWordByte(haystack, start+len(needle)) {
			return true
		}
		offset = start + 1
	}
	return false
}

// isWordByte reports whether s[i] is an ASCII word byte, treating an
// out-of-range index (either end of the string) as a boundary.
func isWordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' ||
		(c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z')
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

// ResolveInstallURL maps a bare catalog skill name to the GitHub tree URL in its
// catalog `source`, so `skills install <name>` fetches the body from wherever the
// skill actually lives instead of the hardcoded <repo>/skills/<name> convention.
// ok is false for inputs that already carry their own location - a full URL or an
// "<org>/<skill>" shorthand, both of which contain "/" or ":" - and for names the
// catalog does not list, leaving the caller's shorthand expansion in charge.
func (c *CatalogClient) ResolveInstallURL(ctx context.Context, input string) (string, bool) {
	if strings.ContainsAny(input, "/:") {
		return "", false
	}
	if entry, ok := c.Lookup(ctx, input); ok && entry.Source != "" {
		return entry.Source, true
	}
	return "", false
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

	sourceURL := SkillTreeURL(c.repository, name)
	if entry, ok := c.Lookup(ctx, name); ok && entry.Source != "" {
		sourceURL = entry.Source
	}

	installer := NewInstaller(c.repository)
	absPath, err := installer.InstallFromGitHub(ctx, sourceURL, destBase, false)
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
