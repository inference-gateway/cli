package memory

import (
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// NewMemoryBackend selects a memory backend from config, mirroring
// storage.NewStorage. The git backend is used only when memory is enabled, the
// backend type is git, and a repo is configured; otherwise the local no-op
// backend is returned so behavior is unchanged. Validation (config.Validate)
// already rejects a git backend without a repo when memory is enabled; the
// guard here is defensive so a partial/legacy config degrades to local rather
// than constructing a broken git backend.
func NewMemoryBackend(cfg *config.Config) domain.MemoryBackend {
	if cfg == nil || !cfg.Memory.Enabled {
		return NewLocalBackend()
	}
	if cfg.Memory.Backend.Type != config.MemoryBackendGit {
		return NewLocalBackend()
	}
	if strings.TrimSpace(cfg.Memory.Backend.Git.Repo) == "" {
		return NewLocalBackend()
	}
	return NewGitBackend(cfg)
}
