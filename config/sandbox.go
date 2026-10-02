package config

import (
	"fmt"
	"os"
	"path/filepath"

	configutils "github.com/inference-gateway/cli/config/utils"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

const SandboxFileName = "sandbox.yaml"

// SandboxConfig is the sandbox policy, one section per resource the tools
// reach. It lives in the userspace sandbox.yaml alone, with no project copy,
// so a checked-out repository can never widen the sandbox of whoever opens it.
type SandboxConfig struct {
	Filesystem FilesystemPolicy `yaml:"filesystem"`
}

// FilesystemPolicy is the paths tools may use and the paths they never may.
// Outside allowed the user is asked.
type FilesystemPolicy struct {
	Allowed []sandboxdomain.Allowed `yaml:"allowed"`
	Denied  []sandboxdomain.Denied  `yaml:"denied"`
}

// DefaultSandboxConfig allows the working directory and /tmp, asks before
// any use of the config dirs, whose files can hold tokens, and denies git
// metadata and the usual credential files.
func DefaultSandboxConfig() *SandboxConfig {
	denied := []sandboxdomain.Denied{{Path: ConfigDirName + "/", OnViolation: sandboxdomain.ViolationApproval}}
	denied = append(denied, sandboxdomain.Deny(
		".git/",
		"*.env",
		".environment",
		"auth.yaml",
		"*.key",
		"*.pem",
		"id_rsa",
		"id_dsa",
		"id_ecdsa",
		"id_ed25519",
	)...)
	return &SandboxConfig{Filesystem: FilesystemPolicy{Allowed: sandboxdomain.Allow(".", "/tmp"), Denied: denied}}
}

// Validate rejects an entry with no path or an unknown access or behaviour.
func (c *SandboxConfig) Validate() error {
	for i, entry := range c.Filesystem.Allowed {
		switch {
		case entry.Path == "":
			return fmt.Errorf("sandbox allowed entry %d has no path", i)
		case !entry.Access.Valid():
			return fmt.Errorf("sandbox allowed %q: access %q must be read or write", entry.Path, entry.Access)
		}
	}
	for i, entry := range c.Filesystem.Denied {
		switch {
		case entry.Path == "":
			return fmt.Errorf("sandbox denied entry %d has no path", i)
		case !entry.OnViolation.Valid():
			return fmt.Errorf("sandbox denied %q: on_violation %q must be block or approval", entry.Path, entry.OnViolation)
		}
	}
	return nil
}

// LoadSandbox reads sandbox.yaml over the defaults, so a missing file or a
// missing list keeps the default one. A list that is present replaces it.
func LoadSandbox(path string) (*SandboxConfig, error) {
	cfg, err := configutils.LoadYAMLMerged(path, "sandbox", DefaultSandboxConfig)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// SaveSandbox writes the sandbox policy to disk, creating parent directories.
func SaveSandbox(path string, cfg *SandboxConfig) error {
	return configutils.SaveYAML(path, "sandbox", cfg)
}

// UserSandboxPath is ~/.infer/sandbox.yaml, the only file the policy is read
// from. The file tools refuse to write it.
func UserSandboxPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	return filepath.Join(home, ConfigDirName, SandboxFileName), nil
}
