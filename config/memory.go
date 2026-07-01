package config

import (
	"fmt"
	"strings"
	"time"

	utils "github.com/inference-gateway/cli/config/utils"
)

const (
	MemoryConfigFileName    = "memory.yaml"
	DefaultMemoryConfigPath = ConfigDirName + "/" + MemoryConfigFileName
)

// Memory backend types selected by memory.backend.type.
const (
	MemoryBackendLocal = "local"
	MemoryBackendGit   = "git"
)

// Memory git sync actions (memory.backend.git.sync.on_start / on_finish).
const (
	MemorySyncPull = "pull"
	MemorySyncPush = "push"
	MemorySyncOff  = "off"
)

// Defaults for the git backend, applied by the Effective* helpers when a
// (possibly partial) memory.yaml leaves them blank - LoadMemory unmarshals into
// a zero struct when the file exists, so struct-level defaults do not merge in.
const (
	DefaultMemoryGitBranch        = "main"
	DefaultMemoryGitCommitMessage = "chore(memory): sync"
	DefaultMemoryGitTimeout       = 60
)

// MemoryConfig configures the agent's persistent, cross-session memory. When
// enabled, durable facts are stored as individual Markdown fact-files under a
// global directory (~/.infer/memory by default), catalogued by an index file
// (MEMORY.md) that the Memory tool keeps in sync. The index is injected into
// context at session start; the agent reads individual facts on demand.
//
// Runtime knobs live here (in memory.yaml); the tool's LLM-facing description
// lives at PromptsConfig.Tools.Memory.Description in prompts.yaml, and the
// session reminder lives in reminders.yaml - keeping each concern in its own
// file, like Heartbeat/ComputerUse/Channels.
type MemoryConfig struct {
	Enabled  bool                `yaml:"enabled" mapstructure:"enabled"`
	Dir      string              `yaml:"dir" mapstructure:"dir"`             // "" => ~/.infer/memory
	MaxChars int                 `yaml:"max_chars" mapstructure:"max_chars"` // cap on the injected MEMORY.md index
	Backend  MemoryBackendConfig `yaml:"backend" mapstructure:"backend"`
}

// MemoryBackendConfig selects how the memory directory is synced. type: local
// (the default) keeps today's behavior exactly (no remote, no-op). type: git
// backs the directory with a git remote: pull on run start, commit + push when
// a fact changes.
type MemoryBackendConfig struct {
	Type string          `yaml:"type" mapstructure:"type"` // local (default) | git
	Git  MemoryGitConfig `yaml:"git" mapstructure:"git"`
}

// MemoryGitConfig configures the git backend. Auth uses the ambient git
// credential chain (ssh-agent, credential helper, GIT_* env) - the backend
// injects no git/ssh env and does not override the ssh command.
type MemoryGitConfig struct {
	Repo          string              `yaml:"repo" mapstructure:"repo"`
	Branch        string              `yaml:"branch" mapstructure:"branch"`
	CommitMessage string              `yaml:"commit_message" mapstructure:"commit_message"`
	Timeout       int                 `yaml:"timeout" mapstructure:"timeout"` // seconds per git op
	Sync          MemoryGitSyncConfig `yaml:"sync" mapstructure:"sync"`
}

// MemoryGitSyncConfig gates the two sync directions. Empty values fall back to
// the enabled defaults (pull / push); "off" disables that direction.
type MemoryGitSyncConfig struct {
	OnStart  string `yaml:"on_start" mapstructure:"on_start"`   // pull (default) | off
	OnFinish string `yaml:"on_finish" mapstructure:"on_finish"` // push (default) | off
}

// DefaultMemoryConfig returns the in-code default memory configuration used when
// no memory.yaml file exists. `infer init` seeds the file from this and the
// runtime falls back to it when the file is absent. Memory is enabled by
// default; the backend defaults to local so behavior is unchanged until a user
// opts into git.
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		Enabled:  true,
		Dir:      "",
		MaxChars: DefaultMemoryMaxChars,
		Backend: MemoryBackendConfig{
			Type: MemoryBackendLocal,
			Git: MemoryGitConfig{
				Repo:          "",
				Branch:        DefaultMemoryGitBranch,
				CommitMessage: DefaultMemoryGitCommitMessage,
				Timeout:       DefaultMemoryGitTimeout,
				Sync: MemoryGitSyncConfig{
					OnStart:  MemorySyncPull,
					OnFinish: MemorySyncPush,
				},
			},
		},
	}
}

// LoadMemory reads memory.yaml from disk. When the file is missing it returns
// the in-code defaults so callers can treat absence as "use defaults".
func LoadMemory(path string) (*MemoryConfig, error) {
	return utils.LoadYAML(path, "memory", DefaultMemoryConfig)
}

// SaveMemory writes the memory configuration to disk, creating any missing
// parent directories.
func SaveMemory(path string, cfg *MemoryConfig) error {
	return utils.SaveYAML(path, "memory", cfg)
}

// Validate checks the memory config, including the backend selection. It is
// called from Config.Validate so a bad backend fails config load rather than
// degrading silently at runtime.
func (m MemoryConfig) Validate() error {
	if m.Enabled && m.MaxChars <= 0 {
		return fmt.Errorf("invalid memory.max_chars %d: must be > 0 when memory is enabled", m.MaxChars)
	}
	return m.Backend.validate(m.Enabled)
}

func (b MemoryBackendConfig) validate(memoryEnabled bool) error {
	switch b.Type {
	case "", MemoryBackendLocal, MemoryBackendGit:
	default:
		return fmt.Errorf("invalid memory.backend.type %q: must be %q or %q", b.Type, MemoryBackendLocal, MemoryBackendGit)
	}
	if b.Type != MemoryBackendGit {
		return nil
	}
	if memoryEnabled && strings.TrimSpace(b.Git.Repo) == "" {
		return fmt.Errorf("memory.backend.git.repo is required when memory.backend.type is %q", MemoryBackendGit)
	}
	switch b.Git.Sync.OnStart {
	case "", MemorySyncPull, MemorySyncOff:
	default:
		return fmt.Errorf("invalid memory.backend.git.sync.on_start %q: must be %q or %q", b.Git.Sync.OnStart, MemorySyncPull, MemorySyncOff)
	}
	switch b.Git.Sync.OnFinish {
	case "", MemorySyncPush, MemorySyncOff:
	default:
		return fmt.Errorf("invalid memory.backend.git.sync.on_finish %q: must be %q or %q", b.Git.Sync.OnFinish, MemorySyncPush, MemorySyncOff)
	}
	return nil
}

// EffectiveBranch returns the configured branch or the default when blank.
func (g MemoryGitConfig) EffectiveBranch() string {
	if b := strings.TrimSpace(g.Branch); b != "" {
		return b
	}
	return DefaultMemoryGitBranch
}

// EffectiveCommitMessage returns the configured commit message or the default.
func (g MemoryGitConfig) EffectiveCommitMessage() string {
	if m := strings.TrimSpace(g.CommitMessage); m != "" {
		return m
	}
	return DefaultMemoryGitCommitMessage
}

// EffectiveTimeout returns the per-operation git timeout, defaulting when unset.
func (g MemoryGitConfig) EffectiveTimeout() time.Duration {
	t := g.Timeout
	if t <= 0 {
		t = DefaultMemoryGitTimeout
	}
	return time.Duration(t) * time.Second
}

// PullOnStart reports whether SyncIn should pull (empty => default on).
func (g MemoryGitConfig) PullOnStart() bool {
	return g.Sync.OnStart == "" || g.Sync.OnStart == MemorySyncPull
}

// PushOnFinish reports whether SyncOut should push (empty => default on).
func (g MemoryGitConfig) PushOnFinish() bool {
	return g.Sync.OnFinish == "" || g.Sync.OnFinish == MemorySyncPush
}
