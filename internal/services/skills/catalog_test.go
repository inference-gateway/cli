package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// catalogServer serves a fixed index.json and counts how many times it was hit.
func catalogServer(t *testing.T, body string) (*httptest.Server, *int) {
	t.Helper()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/"+catalogFile, r.URL.Path)
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func discoveryCfg() *config.Config {
	cfg := enabledCfg()
	cfg.Agent.Skills.Discovery = config.SkillsDiscoveryConfig{Enabled: true}
	return cfg
}

// testCatalog points a client at srv while keeping the default repository, the
// seam every catalog test needs to avoid hitting github.com.
func testCatalog(srv *httptest.Server) *CatalogClient {
	return newCatalogClientWithBase(srv.URL, config.DefaultSkillsRepository)
}

// discoveryService builds a Service whose catalog is served by srv. Discovery
// is enabled unless the caller disables it on the returned config.
func discoveryService(srv *httptest.Server, scopes []scopedDir) *Service {
	s := newWithScopes(discoveryCfg(), scopes)
	s.catalog = testCatalog(srv)
	return s
}

const twoSkillIndex = `{"skills":[{"name":"rust","description":"Idiomatic Rust."},{"name":"local-one","description":"Catalog copy."}]}`

func TestNewCatalogClient_BaseURLFollowsRepository(t *testing.T) {
	cfg := discoveryCfg()
	cfg.Agent.Skills.Repository = "acme/internal-skills"
	require.Equal(t,
		"https://raw.githubusercontent.com/acme/internal-skills/main/",
		NewCatalogClient(cfg).baseURL,
		"the index base must be derived from agent.skills.repository")

	require.Equal(t,
		"https://raw.githubusercontent.com/inference-gateway/skills/main/",
		NewCatalogClient(discoveryCfg()).baseURL,
		"an unset repository must keep the shipped default")
}

func TestSearch_RanksAndCaps(t *testing.T) {
	srv, _ := catalogServer(t, twoSkillIndex)
	c := testCatalog(srv)
	ctx := context.Background()

	hits := c.Search(ctx, "rust", 10)
	require.Len(t, hits, 1)
	require.Equal(t, "rust", hits[0].Name)
	require.Equal(t, domain.SkillScopeCatalog, hits[0].Scope)
	require.Empty(t, hits[0].Path, "search returns metadata only, it must not download")

	require.Len(t, c.Search(ctx, "idiomatic", 10), 1)

	require.Empty(t, c.Search(ctx, "no-such-skill-anywhere", 10))

	require.Len(t, c.Search(ctx, "", 1), 1, "an empty query browses, bounded by limit")
	require.Len(t, c.Search(ctx, "", 0), 2, "a non-positive limit falls back to the ceiling")
}

// Two rounds of over-matching came out of the real catalog: fuzzy over
// descriptions made "rust" match r..u..s..t in any prose, and plain substring
// made "go" match "good" / "algorithm". Names are fuzzy-matched; descriptions
// match on whole words only.
func TestSearch_DescriptionsMatchWholeWordsOnly(t *testing.T) {
	const index = `{"skills":[
		{"name":"rust","description":"Idiomatic Rust."},
		{"name":"pdf","description":"Read unstructured text out of PDF files."},
		{"name":"notes","description":"Mentions rust in passing."}
	]}`
	srv, _ := catalogServer(t, index)

	names := func(hits []domain.Skill) []string {
		out := make([]string, 0, len(hits))
		for _, h := range hits {
			out = append(out, h.Name)
		}
		return out
	}

	c := testCatalog(srv)
	require.Equal(t, []string{"rust", "notes"}, names(c.Search(context.Background(), "rust", 10)),
		"name match ranks first, description matches are whole-word")
}

func TestContainsWord(t *testing.T) {
	tests := []struct {
		haystack string
		needle   string
		want     bool
	}{
		{"idiomatic go - package design", "go", true},
		{"a good algorithm, going forward", "go", false},
		{"scaffold (go/rust/typescript)", "go", true},
		{"ends with go", "go", true},
		{"go leads", "go", true},
		{"handles a pull request well", "pull request", true},
		{"gopher", "go", false},
		{"", "go", false},
		{"anything", "", false},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, containsWord(tt.haystack, tt.needle),
			"containsWord(%q, %q)", tt.haystack, tt.needle)
	}
}

func TestSearch_DescriptionTruncatedToLocalLimit(t *testing.T) {
	long := strings.Repeat("z", skillDescMaxLen*3)
	srv, _ := catalogServer(t, `{"skills":[{"name":"huge","description":"`+long+`"}]}`)

	hits := testCatalog(srv).Search(context.Background(), "huge", 10)
	require.Len(t, hits, 1)
	require.Len(t, hits[0].Description, skillDescMaxLen,
		"a catalog description must be capped like a local one - it lands in the system prompt")
}

func TestIndex_CachedAcrossLookups(t *testing.T) {
	srv, hits := catalogServer(t, twoSkillIndex)

	c := testCatalog(srv)
	entry, found := c.Lookup(context.Background(), "rust")
	require.True(t, found)
	require.Equal(t, "Idiomatic Rust.", entry.Description)

	_, found = c.Lookup(context.Background(), "missing")
	require.False(t, found)

	require.Equal(t, 1, *hits, "index should be fetched once and cached")
}

func TestIndex_TokenOnlySentToGitHubHosts(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "s3cret")
	t.Setenv("GH_TOKEN", "")

	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(twoSkillIndex))
	}))
	t.Cleanup(srv.Close)

	_, found := testCatalog(srv).Lookup(context.Background(), "rust")
	require.True(t, found)
	require.Empty(t, auth, "token must not leak to a non-GitHub index host")

	require.True(t, isGitHubHost("raw.githubusercontent.com"))
	require.True(t, isGitHubHost("github.com:443"))
	require.False(t, isGitHubHost("evil-github.com"))
	require.False(t, isGitHubHost("127.0.0.1:1234"))
}

func TestLoad_MergesCatalogEntries(t *testing.T) {
	srv, _ := catalogServer(t, twoSkillIndex)
	tmp := t.TempDir()
	writeSkill(t, tmp, "local-one", validSkillBody("local-one", "The local copy wins."))

	s := discoveryService(srv, scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	local, ok := s.Get("local-one")
	require.True(t, ok)
	require.Equal(t, domain.SkillScopeProject, local.Scope, "local skill must not be shadowed by the catalog")
	require.Equal(t, "The local copy wins.", local.Description)

	remote, ok := s.Get("rust")
	require.True(t, ok, "catalog skill should be listed after Load")
	require.Equal(t, domain.SkillScopeCatalog, remote.Scope)
	require.Empty(t, remote.Path, "catalog entry is metadata-only until invoked")
}

func TestLoad_CatalogSkippedWhenDiscoveryDisabled(t *testing.T) {
	srv, hits := catalogServer(t, twoSkillIndex)

	s := discoveryService(srv, scope(t.TempDir()))
	s.cfg.Agent.Skills.Discovery.Enabled = false

	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Zero(t, *hits, "no registry traffic when discovery is off")
}

func TestLoad_CatalogFailureIsNotFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	tmp := t.TempDir()
	writeSkill(t, tmp, "local-one", validSkillBody("local-one", "Still loads."))

	s := discoveryService(srv, scope(tmp))
	require.NoError(t, s.Load(context.Background()))
	require.Len(t, s.List(), 1)
}

func TestDiscover_LocalSkillNeedsNoDiscovery(t *testing.T) {
	tmp := t.TempDir()
	path := writeSkill(t, tmp, "local-one", validSkillBody("local-one", "Local."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	sk, ok := s.Discover(context.Background(), "local-one")
	require.True(t, ok)
	require.Equal(t, path, sk.Path)

	_, ok = s.Discover(context.Background(), "rust")
	require.False(t, ok)
}

func TestDiscover_DownloadFailureLeavesPlaceholder(t *testing.T) {
	srv, _ := catalogServer(t, twoSkillIndex)
	s := discoveryService(srv, scope(t.TempDir()))
	require.NoError(t, s.Load(context.Background()))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, ok := s.Discover(ctx, "rust")
	require.False(t, ok)

	placeholder, found := s.Get("rust")
	require.True(t, found)
	require.Empty(t, placeholder.Path)
}
