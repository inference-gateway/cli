package plugins

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"
)

func validSkillBody(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Body\n"
}

const ponytailManifest = `{
  "name": "ponytail",
  "version": "4.8.4",
  "description": "Lazy senior dev mode.",
  "author": {"name": "Dietrich Gebert", "url": "https://github.com/DietrichGebert"},
  "hooks": "./hooks/claude-codex-hooks.json"
}`

type fakeRepo struct {
	Files     map[string]string
	Truncated bool
	Status    int
}

func newMockServer(t *testing.T, repo fakeRepo) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		if repo.Status != 0 {
			w.WriteHeader(repo.Status)
			return
		}
		if !strings.Contains(r.URL.Path, "/git/trees/") {
			http.NotFound(w, r)
			return
		}
		var entries []treeEntry
		for p := range repo.Files {
			entries = append(entries, treeEntry{Path: p, Type: "blob"})
		}
		resp := treeResponse{Tree: entries, Truncated: repo.Truncated}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
		if len(parts) < 4 {
			http.NotFound(w, r)
			return
		}
		body, ok := repo.Files[parts[3]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func newTestInstaller(serverURL string) *Installer {
	return &Installer{Client: http.DefaultClient, APIBase: serverURL, RawBase: serverURL}
}

func ponytailRepo() fakeRepo {
	return fakeRepo{Files: map[string]string{
		".claude-plugin/plugin.json":      ponytailManifest,
		"AGENTS.md":                       "# Ponytail\nBe lazy.",
		"skills/ponytail/SKILL.md":        validSkillBody("ponytail", "Lazy senior dev mode."),
		"skills/ponytail-review/SKILL.md": validSkillBody("ponytail-review", "Audit the diff."),
		"hooks/claude-codex-hooks.json":   "{}",
		"hooks/ponytail-activate.js":      "console.log('nope')",
		"commands/ponytail.toml":          `description = "x"`,
		"README.md":                       "readme",
		"benchmarks/data.json":            "{}",
	}}
}

func TestStageGitHub_DownloadsOnlyMappedSubset(t *testing.T) {
	srv := newMockServer(t, ponytailRepo())
	defer srv.Close()

	staging := filepath.Join(t.TempDir(), "staging")
	src, err := ParseSource("DietrichGebert/ponytail")
	require.NoError(t, err)

	unsupported, err := newTestInstaller(srv.URL).Stage(context.Background(), src, staging)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(staging, ".claude-plugin", "plugin.json"))
	require.FileExists(t, filepath.Join(staging, "AGENTS.md"))
	require.FileExists(t, filepath.Join(staging, "skills", "ponytail", "SKILL.md"))
	require.NoFileExists(t, filepath.Join(staging, "hooks", "ponytail-activate.js"))
	require.NoFileExists(t, filepath.Join(staging, "commands", "ponytail.toml"))
	require.NoFileExists(t, filepath.Join(staging, "README.md"))

	require.Equal(t, 2, unsupported["hooks"])
	require.Equal(t, 1, unsupported["commands"])
}

func TestStageGitHub_NoMappableContent(t *testing.T) {
	srv := newMockServer(t, fakeRepo{Files: map[string]string{"README.md": "just a readme"}})
	defer srv.Close()

	src, _ := ParseSource("o/r")
	_, err := newTestInstaller(srv.URL).Stage(context.Background(), src, t.TempDir())
	require.ErrorContains(t, err, "nothing to install")
}

func TestStageGitHub_ErrorTaxonomy(t *testing.T) {
	tests := []struct {
		name    string
		repo    fakeRepo
		wantErr string
	}{
		{"not found", fakeRepo{Status: http.StatusNotFound}, "repository or ref not found"},
		{"rate limited", fakeRepo{Status: http.StatusForbidden}, "rate limit"},
		{"truncated", fakeRepo{Files: map[string]string{"AGENTS.md": "x"}, Truncated: true}, "truncated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newMockServer(t, tt.repo)
			defer srv.Close()
			src, _ := ParseSource("o/r")
			_, err := newTestInstaller(srv.URL).Stage(context.Background(), src, t.TempDir())
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestStageGitHub_RejectsTraversalPaths(t *testing.T) {
	repo := fakeRepo{Files: map[string]string{
		"skills/../../evil/SKILL.md": "boom",
		"AGENTS.md":                  "x",
	}}
	srv := newMockServer(t, repo)
	defer srv.Close()

	src, _ := ParseSource("o/r")
	_, err := newTestInstaller(srv.URL).Stage(context.Background(), src, t.TempDir())
	require.ErrorContains(t, err, "unsafe path")
}

func TestStageLocal_CopiesMappedSubset(t *testing.T) {
	srcDir := t.TempDir()
	for p, body := range ponytailRepo().Files {
		full := filepath.Join(srcDir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	staging := filepath.Join(t.TempDir(), "staging")
	src, err := ParseSource(srcDir)
	require.NoError(t, err)
	require.Equal(t, SourceLocal, src.Kind)

	unsupported, err := NewInstaller().Stage(context.Background(), src, staging)
	require.NoError(t, err)
	require.FileExists(t, filepath.Join(staging, "AGENTS.md"))
	require.FileExists(t, filepath.Join(staging, "skills", "ponytail", "SKILL.md"))
	require.NoFileExists(t, filepath.Join(staging, "hooks", "ponytail-activate.js"))
	require.Equal(t, 2, unsupported["hooks"])
}

func TestInspect_ManifestAndSkills(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		".claude-plugin/plugin.json": ponytailManifest,
		"AGENTS.md":                  "Be lazy.",
		"skills/ponytail/SKILL.md":   validSkillBody("ponytail", "Lazy mode."),
		"skills/broken/SKILL.md":     "---\ndescription: no name\n---\n",
	}
	for p, body := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}

	res, err := Inspect(dir, "fallback")
	require.NoError(t, err)
	require.Equal(t, "ponytail", res.Name, "manifest name wins over fallback")
	require.Equal(t, "4.8.4", res.Version)
	require.True(t, res.HasInstructions)
	require.Len(t, res.Skills, 1)
	require.Len(t, res.SkillErrors, 1, "invalid skill is a warning, not fatal")
}

func TestInspect_NoManifestFallsBackToLayout(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("rules"), 0o644))

	res, err := Inspect(dir, "My.Repo Name")
	require.NoError(t, err)
	require.Equal(t, "my-repo-name", res.Name, "fallback name is sanitized")
	require.True(t, res.HasInstructions)
}

func TestInspect_NoContentErrors(t *testing.T) {
	_, err := Inspect(t.TempDir(), "empty")
	require.ErrorContains(t, err, "no installable content")
}

func TestCommit_OverwriteRestoresOnFailureSemantics(t *testing.T) {
	root := t.TempDir()
	final := filepath.Join(root, "p")
	require.NoError(t, os.MkdirAll(final, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(final, "old.txt"), []byte("old"), 0o644))

	staging := filepath.Join(root, ".staging-x")
	require.NoError(t, os.MkdirAll(staging, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(staging, "new.txt"), []byte("new"), 0o644))

	require.Error(t, Commit(staging, final, false), "existing dir without overwrite must fail")
	require.FileExists(t, filepath.Join(final, "old.txt"))

	require.NoError(t, Commit(staging, final, true))
	require.FileExists(t, filepath.Join(final, "new.txt"))
	require.NoFileExists(t, filepath.Join(final, "old.txt"))
	require.NoDirExists(t, final+".trash")
	require.NoDirExists(t, staging)
}

func TestUninstall_GuardsName(t *testing.T) {
	root := t.TempDir()
	_, err := Uninstall("../evil", root)
	require.ErrorContains(t, err, "invalid plugin name")

	require.NoError(t, os.MkdirAll(filepath.Join(root, "good"), 0o755))
	dir, err := Uninstall("good", root)
	require.NoError(t, err)
	require.NoDirExists(t, dir)

	_, err = Uninstall("gone", root)
	require.NoError(t, err, "missing dir heals silently")
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		in      string
		want    Source
		wantErr string
	}{
		{in: "owner/repo", want: Source{Kind: SourceGitHub, Owner: "owner", Repo: "repo", Raw: "owner/repo"}},
		{in: "owner/repo@v1.2.3", want: Source{Kind: SourceGitHub, Owner: "owner", Repo: "repo", Ref: "v1.2.3", Raw: "owner/repo"}},
		{in: "https://github.com/o/r", want: Source{Kind: SourceGitHub, Owner: "o", Repo: "r", Raw: "o/r"}},
		{in: "https://github.com/o/r/tree/dev", want: Source{Kind: SourceGitHub, Owner: "o", Repo: "r", Ref: "dev", Raw: "o/r"}},
		{in: "https://github.com/o/r/blob/main/x.md", wantErr: "/blob/"},
		{in: "https://gitlab.com/o/r", wantErr: "only github.com"},
		{in: "a/b/c", wantErr: "must be <owner>/<repo>"},
		{in: "owner/repo@", wantErr: "empty ref"},
		{in: "", wantErr: "empty plugin source"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseSource(tt.in)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSanitizePluginName(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "ponytail", want: "ponytail"},
		{in: "Ponytail.Skills", want: "ponytail-skills"},
		{in: "My Repo_Name", want: "my-repo-name"},
		{in: "---", wantErr: true},
		{in: strings.Repeat("a", 65), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := sanitizePluginName(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestSourceEffectiveRef(t *testing.T) {
	require.Equal(t, "HEAD", Source{}.EffectiveRef())
	require.Equal(t, "dev", Source{Ref: "dev"}.EffectiveRef())
}
