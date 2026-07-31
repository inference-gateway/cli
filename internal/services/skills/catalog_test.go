package skills

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func discoveryCfg(registryURL string) *config.Config {
	cfg := enabledCfg()
	cfg.Agent.Skills.Discovery = config.SkillsDiscoveryConfig{
		Enabled:     true,
		RegistryURL: registryURL,
	}
	return cfg
}

const twoSkillIndex = `{"skills":[{"name":"rust","description":"Idiomatic Rust."},{"name":"local-one","description":"Catalog copy."}]}`

func TestIndex_CachedAcrossLookups(t *testing.T) {
	srv, hits := catalogServer(t, twoSkillIndex)

	c := NewCatalogClient(discoveryCfg(srv.URL))
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

	// httptest listens on 127.0.0.1 - a third-party registry must not see it.
	_, found := NewCatalogClient(discoveryCfg(srv.URL)).Lookup(context.Background(), "rust")
	require.True(t, found)
	require.Empty(t, auth, "token must not leak to a non-GitHub registry_url")

	require.True(t, isGitHubHost("raw.githubusercontent.com"))
	require.True(t, isGitHubHost("github.com:443"))
	require.False(t, isGitHubHost("evil-github.com"))
	require.False(t, isGitHubHost("127.0.0.1:1234"))
}

func TestLoad_MergesCatalogEntries(t *testing.T) {
	srv, _ := catalogServer(t, twoSkillIndex)
	tmp := t.TempDir()
	writeSkill(t, tmp, "local-one", validSkillBody("local-one", "The local copy wins."))

	s := newWithScopes(discoveryCfg(srv.URL), scope(tmp))
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

	cfg := enabledCfg()
	cfg.Agent.Skills.Discovery.RegistryURL = srv.URL

	s := newWithScopes(cfg, scope(t.TempDir()))
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

	s := newWithScopes(discoveryCfg(srv.URL), scope(tmp))
	require.NoError(t, s.Load(context.Background()))
	require.Len(t, s.List(), 1)
}

func TestDiscover_LocalSkillNeedsNoDiscovery(t *testing.T) {
	tmp := t.TempDir()
	path := writeSkill(t, tmp, "local-one", validSkillBody("local-one", "Local."))

	// Discovery disabled: an already-loaded skill must still resolve, since
	// Discover is what the activation path calls.
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
	s := newWithScopes(discoveryCfg(srv.URL), scope(t.TempDir()))
	require.NoError(t, s.Load(context.Background()))

	// The download targets the canonical GitHub repo; with a cancelled context
	// it fails, and the placeholder must survive rather than being replaced by
	// a half-built entry.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, ok := s.Discover(ctx, "rust")
	require.False(t, ok)

	placeholder, found := s.Get("rust")
	require.True(t, found)
	require.Empty(t, placeholder.Path)
}
