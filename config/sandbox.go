package config

import (
	"fmt"
	"os"
	"path/filepath"

	configutils "github.com/inference-gateway/cli/config/utils"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

const (
	SandboxFileName    = "sandbox.yaml"
	DefaultSandboxPath = ConfigDirName + "/" + SandboxFileName
)

// SandboxConfig is the sandbox policy: the paths tools may use and the paths
// they never may. Outside allowed the user is asked. It lives in its own
// sandbox.yaml so the policy can be reviewed on its own and the agent's file
// tools can never edit it.
type SandboxConfig struct {
	Allowed []sandboxdomain.Allowed `yaml:"allowed"`
	Denied  []sandboxdomain.Denied  `yaml:"denied"`
}

// DefaultSandboxConfig allows the working directory and /tmp and denies the
// config dir, git metadata and the usual credential files.
func DefaultSandboxConfig() *SandboxConfig {
	denied := sandboxdomain.Deny(
		ConfigDirName+"/",
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
	return &SandboxConfig{Allowed: sandboxdomain.Allow(".", "/tmp"), Denied: denied}
}

// Validate rejects an entry with no path or an unknown access or behaviour.
func (c *SandboxConfig) Validate() error {
	for i, entry := range c.Allowed {
		switch {
		case entry.Path == "":
			return fmt.Errorf("sandbox allowed entry %d has no path", i)
		case entry.Access != sandboxdomain.AccessRead && entry.Access != sandboxdomain.AccessWrite:
			return fmt.Errorf("sandbox allowed %q: access %q must be read or write", entry.Path, entry.Access)
		}
	}
	for i, entry := range c.Denied {
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

// SandboxFilePaths are the files the policy may be read from, project first
// then userspace. The file tools refuse to write any of them.
func SandboxFilePaths() []string {
	paths := []string{DefaultSandboxPath}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ConfigDirName, SandboxFileName))
	}
	return paths
}
