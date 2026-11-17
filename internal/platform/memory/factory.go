package memory

import (
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// NewMemoryBackend selects a memory backend from config, like storage.NewStorage.
// It returns the git backend only when memory is enabled with a git backend and
// a repo, and the local no-op backend otherwise. config.Validate already rejects
// a git backend without a repo, so this guard only keeps legacy configs safe.
func NewMemoryBackend(cfg *config.Config) MemoryBackend {
	if cfg == nil || !cfg.Memory.Enabled {
		return NewLocalBackend()
	}
	if cfg.Memory.Backend.Type != config.MemoryBackendGit {
		return NewLocalBackend()
	}
	if strings.TrimSpace(cfg.Memory.Backend.Git.Repo) == "" {
		logger.Warn("memory sync backend: git selected but repo is empty; falling back to local no-op")
		return NewLocalBackend()
	}
	return NewGitBackend(cfg)
}
