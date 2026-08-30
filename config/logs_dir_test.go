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

func TestInferGitignoreContent(t *testing.T) {
	for _, forbidden := range []string{"bin/", "logs/*.log"} {
		if strings.Contains(config.InferGitignoreContent, forbidden) {
			t.Errorf("seeded gitignore lists %q; gateway binary and logs are machine-scoped and live under ~/.infer", forbidden)
		}
	}
}
