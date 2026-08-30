package export

import (
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestExportOutputDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name      string
		outputDir string
		want      string
	}{
		{"unset falls back to the project runtime exports dir", "", filepath.Join(config.ProjectRuntimeDir(), "exports")},
		{"explicit config value wins", "/somewhere/else", "/somewhere/else"},
		{"explicit relative value wins", "docs/exports", "docs/exports"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Export: config.ExportConfig{OutputDir: tt.outputDir}}
			if got := exportOutputDir(cfg); got != tt.want {
				t.Errorf("exportOutputDir() = %q, want %q", got, tt.want)
			}
		})
	}
}
