package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// wantSlug derives the expected runtime slug the same way projectRuntimeSlug
// does - from os.Getwd(), not EvalSymlinks - so a checkout behind a symlink
// does not make these tests disagree with the implementation.
func wantSlug(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return strings.ReplaceAll(cwd, string(filepath.Separator), "-")
}

func TestProjectRuntimeDir(t *testing.T) {
	t.Run("defaults to ~/.infer/projects/<cwd-slug>", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		want := filepath.Join(home, ".infer", "projects", wantSlug(t))
		if got := config.ProjectRuntimeDir(); got != want {
			t.Fatalf("ProjectRuntimeDir() = %q, want %q", got, want)
		}
	})

	t.Run("cwd inside ~/.infer slugs to its relative path", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		workspace := filepath.Join(home, ".infer", "workspace")
		if err := os.MkdirAll(workspace, 0755); err != nil {
			t.Fatal(err)
		}
		t.Chdir(workspace)

		want := filepath.Join(home, ".infer", "projects", "workspace")
		if got := config.ProjectRuntimeDir(); got != want {
			t.Fatalf("ProjectRuntimeDir() = %q, want %q", got, want)
		}
	})

	t.Run("tmp scratch lives under the project runtime root", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		want := filepath.Join(home, ".infer", "projects", wantSlug(t), "tmp")
		if got := config.ProjectTmpDir(); got != want {
			t.Fatalf("ProjectTmpDir() = %q, want %q", got, want)
		}
	})

	t.Run("userspace config dir is the runtime root's parent", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		if got, want := config.UserSpaceConfigDir(), filepath.Join(home, ".infer"); got != want {
			t.Fatalf("UserSpaceConfigDir() = %q, want %q", got, want)
		}
	})
}

func TestMediaDir(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	workspace := filepath.Join(home, ".infer", "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}
	userspace := filepath.Join(home, ".infer", "tmp", "media", "sfx")

	tests := []struct {
		name string
		cwd  string
		want func() string
	}{
		{"open project uses its runtime tmp", project, func() string { return filepath.Join(config.ProjectTmpDir(), "media", "sfx") }},
		{"home falls back to userspace", home, func() string { return userspace }},
		{"desktop workspace falls back to userspace", workspace, func() string { return userspace }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", home)
			t.Chdir(tt.cwd)

			got, err := config.MediaDir("sfx")
			if err != nil {
				t.Fatal(err)
			}
			if want := tt.want(); got != want {
				t.Fatalf("MediaDir(sfx) = %q, want %q", got, want)
			}
			if tt.cwd == project && !strings.Contains(got, filepath.Join(".infer", "projects")) {
				t.Fatalf("MediaDir(sfx) = %q, want it under ~/.infer/projects", got)
			}
		})
	}
}
