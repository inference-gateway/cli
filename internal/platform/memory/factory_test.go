package memory

import (
	"testing"

	config "github.com/inference-gateway/cli/config"
)

func TestNewMemoryBackend_Selection(t *testing.T) {
	gitCfg := func(mutate func(*config.Config)) *config.Config {
		cfg := config.DefaultConfig()
		cfg.Memory.Enabled = true
		cfg.Memory.Backend.Type = config.MemoryBackendGit
		cfg.Memory.Backend.Git.Repo = "git@github.com:org/agent-memory.git"
		if mutate != nil {
			mutate(cfg)
		}
		return cfg
	}

	tests := []struct {
		name    string
		cfg     *config.Config
		wantGit bool
	}{
		{"nil config", nil, false},
		{"git backend with repo", gitCfg(nil), true},
		{"memory disabled", gitCfg(func(c *config.Config) { c.Memory.Enabled = false }), false},
		{"local type", gitCfg(func(c *config.Config) { c.Memory.Backend.Type = config.MemoryBackendLocal }), false},
		{"git type without repo", gitCfg(func(c *config.Config) { c.Memory.Backend.Git.Repo = "" }), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewMemoryBackend(tt.cfg)
			_, isGit := backend.(*GitBackend)
			if isGit != tt.wantGit {
				t.Fatalf("NewMemoryBackend gitBackend=%v, want %v (got %T)", isGit, tt.wantGit, backend)
			}
		})
	}
}
