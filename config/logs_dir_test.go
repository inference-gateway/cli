package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestDefaultLogsDir(t *testing.T) {
	t.Run("defaults to userspace ~/.infer/logs", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		if got, want := config.DefaultLogsDir(), filepath.Join(home, ".infer", "logs"); got != want {
			t.Fatalf("DefaultLogsDir() = %q, want %q", got, want)
		}
	})
}

func TestProjectRuntimeDir(t *testing.T) {
	t.Run("defaults to ~/.infer/projects/<cwd-slug>", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		cwd, err := filepath.EvalSymlinks(".")
		if err != nil {
			t.Fatalf("eval symlinks: %v", err)
		}
		cwd, _ = filepath.Abs(cwd)
		slug := strings.ReplaceAll(cwd, string(filepath.Separator), "-")

		want := filepath.Join(home, ".infer", "projects", slug)
		if got := config.ProjectRuntimeDir(); got != want {
			t.Fatalf("ProjectRuntimeDir() = %q, want %q", got, want)
		}
	})

	t.Run("tmp scratch lives under the project runtime root", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)

		cwd, _ := filepath.Abs(".")
		slug := strings.ReplaceAll(cwd, string(filepath.Separator), "-")

		want := filepath.Join(home, ".infer", "projects", slug, "tmp")
		if got := config.ProjectTmpDir(); got != want {
			t.Fatalf("ProjectTmpDir() = %q, want %q", got, want)
		}
	})
}
