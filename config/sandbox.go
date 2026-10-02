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

// DefaultSandboxConfig allows the working directory, /tmp and the userspace
// scratch dir, keeps the config dirs read-only, and denies git metadata and
// the usual credential files. Narrower allowed entries come first because the
// first match wins.
func DefaultSandboxConfig() *SandboxConfig {
	allowed := []sandboxdomain.Allowed{
		{Path: "~/" + ConfigDirName + "/tmp", Access: sandboxdomain.AccessWrite},
		{Path: ConfigDirName + "/", Access: sandboxdomain.AccessRead},
	}
	allowed = append(allowed, sandboxdomain.Allow(".", "/tmp", "/private/tmp")...)
	denied := sandboxdomain.Deny(
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
	)
	return &SandboxConfig{Filesystem: FilesystemPolicy{Allowed: allowed, Denied: denied}}
}

// Validate rejects an entry with no path or an unknown access or behaviour.
func (c *SandboxConfig) Validate() error {
	for i, entry := range c.Filesystem.Allowed {
		switch {
		case entry.Path == "":
			return fmt.Errorf("sandbox allowed entry %d has no path", i)
		case entry.Access != sandboxdomain.AccessRead && entry.Access != sandboxdomain.AccessWrite:
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

// LoadSandbox reads sandbox.yaml. A missing file is the defaults. A present
// file replaces the policy wholesale, so it is exactly what it says.
func LoadSandbox(path string) (*SandboxConfig, error) {
	cfg, err := configutils.LoadYAML(path, "sandbox", DefaultSandboxConfig)
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
