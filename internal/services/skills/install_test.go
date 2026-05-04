package skills

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

func TestExpandShorthand(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "single segment uses default org and skills repo",
			input: "skill-creator",
			want:  "https://github.com/inference-gateway/skills/tree/main/skills/skill-creator",
		},
		{
			name:  "two segments use given org and skills repo",
			input: "acme/foo",
			want:  "https://github.com/acme/skills/tree/main/skills/foo",
		},
		{
			name:  "https URL is returned unchanged",
			input: "https://github.com/anthropics/skills/tree/main/skills/pdf",
			want:  "https://github.com/anthropics/skills/tree/main/skills/pdf",
		},
		{
			name:  "http URL is returned unchanged",
			input: "http://example.com/x",
			want:  "http://example.com/x",
		},
		{
			name:  "empty input is returned unchanged",
			input: "",
			want:  "",
		},
		{
			name:  "three segments fall through unchanged",
			input: "a/b/c",
			want:  "a/b/c",
		},
		{
			name:  "leading and trailing slashes are trimmed",
			input: "/skill/",
			want:  "https://github.com/inference-gateway/skills/tree/main/skills/skill",
		},
		{
			name:  "empty middle segment falls through unchanged",
			input: "a//b",
			want:  "a//b",
		},
		{
			name:  "single slash falls through unchanged",
			input: "/",
			want:  "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, ExpandShorthand(tt.input))
		})
	}
}

func TestParseGitHubTreeURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
		want    *GitHubLocation
	}{
		{
			name: "happy path single-segment ref",
			url:  "https://github.com/anthropics/skills/tree/main/skills/pdf",
			want: &GitHubLocation{Owner: "anthropics", Repo: "skills", Ref: "main", Path: "skills/pdf"},
		},
		{
			name: "happy path nested path",
			url:  "https://github.com/foo/bar/tree/v1.2.3/a/b/c/d",
			want: &GitHubLocation{Owner: "foo", Repo: "bar", Ref: "v1.2.3", Path: "a/b/c/d"},
		},
		{
			name:    "blob URL rejected",
			url:     "https://github.com/anthropics/skills/blob/main/skills/pdf/SKILL.md",
			wantErr: "/blob/",
		},
		{
			name:    "repo root URL rejected",
			url:     "https://github.com/anthropics/skills",
			wantErr: "tree/<ref>/<path>",
		},
		{
			name:    "tree without path rejected",
			url:     "https://github.com/anthropics/skills/tree/main",
			wantErr: "tree/<ref>/<path>",
		},
		{
			name:    "non-github host rejected",
			url:     "https://gitlab.com/foo/bar/tree/main/skills/pdf",
			wantErr: "github.com",
		},
		{
			name:    "non-https scheme rejected",
			url:     "ftp://github.com/foo/bar/tree/main/skills/pdf",
			wantErr: "http(s)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitHubTreeURL(tt.url)
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// fakeRepo describes a tree the mock server should serve plus the file
// bodies. Keys in Files are paths inside the repo (matching the tree
// entries' Path field).
type fakeRepo struct {
	Truncated bool
	Files     map[string]string // repoPath -> body
	Trees     map[string]bool   // repoPath -> exists (for "tree" entries)
}

func newMockServer(t *testing.T, repo fakeRepo) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		// path looks like /repos/<owner>/<repo>/git/trees/<ref>
		if !strings.Contains(r.URL.Path, "/git/trees/") {
			http.NotFound(w, r)
			return
		}
		var entries []treeEntry
		for p := range repo.Files {
			entries = append(entries, treeEntry{Path: p, Type: "blob"})
		}
		for p := range repo.Trees {
			entries = append(entries, treeEntry{Path: p, Type: "tree"})
		}
		resp := treeResponse{Tree: entries, Truncated: repo.Truncated}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// Raw file fetches: /<owner>/<repo>/<ref>/<path>
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 4)
		if len(parts) < 4 {
			http.NotFound(w, r)
			return
		}
		repoPath := parts[3]
		body, ok := repo.Files[repoPath]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})

	return httptest.NewServer(mux)
}

func newTestInstaller(serverURL string) *Installer {
	return &Installer{
		Client:  http.DefaultClient,
		APIBase: serverURL,
		RawBase: serverURL,
	}
}

func TestInstallFromGitHub_HappyPath(t *testing.T) {
	repo := fakeRepo{
		Files: map[string]string{
			"skills/pdf/SKILL.md":       validSkillBody("pdf", "Use this skill to work with PDFs."),
			"skills/pdf/reference.md":   "# Reference\n",
			"skills/pdf/scripts/run.sh": "#!/bin/sh\necho hi\n",
			"skills/other/SKILL.md":     "should not be downloaded",
		},
		Trees: map[string]bool{
			"skills":             true,
			"skills/pdf":         true,
			"skills/pdf/scripts": true,
			"skills/other":       true,
		},
	}
	srv := newMockServer(t, repo)
	defer srv.Close()

	dest := t.TempDir()
	inst := newTestInstaller(srv.URL)

	got, err := inst.InstallFromGitHub(context.Background(),
		"https://github.com/anthropics/skills/tree/main/skills/pdf",
		dest, false)
	require.NoError(t, err)

	skillDir := filepath.Join(dest, "pdf")
	abs, _ := filepath.Abs(skillDir)
	require.Equal(t, abs, got)

	require.FileExists(t, filepath.Join(skillDir, "SKILL.md"))
	require.FileExists(t, filepath.Join(skillDir, "reference.md"))
	require.FileExists(t, filepath.Join(skillDir, "scripts", "run.sh"))

	// Sibling skill must not be downloaded.
	_, err = os.Stat(filepath.Join(dest, "other"))
	require.True(t, os.IsNotExist(err))
}

func TestInstallFromGitHub_ShorthandResolves(t *testing.T) {
	// Shorthand "acme/skill-creator" should resolve to the "skills" repo
	// under the acme org with path "skills/skill-creator". The mock server
	// doesn't care about owner/repo/ref segments — it just looks at the
	// repo path prefix — so we need a SKILL.md at
	// skills/skill-creator/SKILL.md to match the resolved tree path.
	repo := fakeRepo{
		Files: map[string]string{
			"skills/skill-creator/SKILL.md": validSkillBody("skill-creator", "Test skill."),
		},
	}
	srv := newMockServer(t, repo)
	defer srv.Close()

	dest := t.TempDir()
	got, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"acme/skill-creator", dest, false)
	require.NoError(t, err)

	abs, _ := filepath.Abs(filepath.Join(dest, "skill-creator"))
	require.Equal(t, abs, got)
	require.FileExists(t, filepath.Join(dest, "skill-creator", "SKILL.md"))
}

func TestInstallFromGitHub_RepoNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	dest := t.TempDir()
	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", dest, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestInstallFromGitHub_RateLimited(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusForbidden)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", t.TempDir(), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit")
}

func TestInstallFromGitHub_TreeTruncated(t *testing.T) {
	repo := fakeRepo{Truncated: true, Files: map[string]string{}}
	srv := newMockServer(t, repo)
	defer srv.Close()

	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", t.TempDir(), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "truncated")
}

func TestInstallFromGitHub_NoFilesUnderPath(t *testing.T) {
	repo := fakeRepo{
		Files: map[string]string{"unrelated/file.md": "x"},
	}
	srv := newMockServer(t, repo)
	defer srv.Close()

	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", t.TempDir(), false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no files found")
}

func TestInstallFromGitHub_InvalidFrontmatterRollsBack(t *testing.T) {
	// SKILL.md exists but `name` doesn't match dirname — validator rejects.
	repo := fakeRepo{
		Files: map[string]string{
			"skills/pdf/SKILL.md": validSkillBody("not-pdf", "wrong name"),
		},
	}
	srv := newMockServer(t, repo)
	defer srv.Close()

	dest := t.TempDir()
	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", dest, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "validation")

	_, statErr := os.Stat(filepath.Join(dest, "pdf"))
	require.True(t, os.IsNotExist(statErr), "destination should be cleaned up after validation failure")
}

func TestInstallFromGitHub_MissingSkillMD(t *testing.T) {
	// Files exist under the prefix but none is SKILL.md.
	repo := fakeRepo{
		Files: map[string]string{
			"skills/pdf/reference.md": "# refs",
		},
	}
	srv := newMockServer(t, repo)
	defer srv.Close()

	dest := t.TempDir()
	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", dest, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SKILL.md")

	_, statErr := os.Stat(filepath.Join(dest, "pdf"))
	require.True(t, os.IsNotExist(statErr))
}

func TestInstallFromGitHub_ExistingDirRequiresOverwrite(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "pdf"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "pdf", "marker"), []byte("old"), 0o644))

	srv := newMockServer(t, fakeRepo{Files: map[string]string{
		"skills/pdf/SKILL.md": validSkillBody("pdf", "ok"),
	}})
	defer srv.Close()

	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", dest, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--overwrite")

	// marker still there.
	_, statErr := os.Stat(filepath.Join(dest, "pdf", "marker"))
	require.NoError(t, statErr)
}

func TestUninstall_HappyPath(t *testing.T) {
	dest := t.TempDir()
	skillDir := filepath.Join(dest, "pdf")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: pdf\n---\n"), 0o644))

	got, err := Uninstall("pdf", dest)
	require.NoError(t, err)
	abs, _ := filepath.Abs(skillDir)
	require.Equal(t, abs, got)

	_, statErr := os.Stat(skillDir)
	require.True(t, os.IsNotExist(statErr))
}

func TestUninstall_NotFound(t *testing.T) {
	_, err := Uninstall("missing", t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestUninstall_InvalidNameRejected(t *testing.T) {
	tests := []string{
		"../etc",
		"a/b",
		"UPPER",
		"with space",
		"under_score",
		"",
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Uninstall(name, t.TempDir())
			require.Error(t, err)
			require.Contains(t, err.Error(), "invalid skill name")
		})
	}
}

func TestUninstall_RejectsFile(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dest, "pdf"), []byte("not a dir"), 0o644))

	_, err := Uninstall("pdf", dest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a directory")
}

func TestInstallFromGitHub_OverwriteReplaces(t *testing.T) {
	dest := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "pdf"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dest, "pdf", "marker"), []byte("old"), 0o644))

	srv := newMockServer(t, fakeRepo{Files: map[string]string{
		"skills/pdf/SKILL.md": validSkillBody("pdf", "fresh"),
	}})
	defer srv.Close()

	_, err := newTestInstaller(srv.URL).InstallFromGitHub(context.Background(),
		"https://github.com/foo/bar/tree/main/skills/pdf", dest, true)
	require.NoError(t, err)

	// marker gone, SKILL.md present.
	_, statErr := os.Stat(filepath.Join(dest, "pdf", "marker"))
	require.True(t, os.IsNotExist(statErr))
	body, err := os.ReadFile(filepath.Join(dest, "pdf", "SKILL.md"))
	require.NoError(t, err)
	require.Contains(t, string(body), "fresh")
}
