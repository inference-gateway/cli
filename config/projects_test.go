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
