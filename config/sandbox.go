package config

import (
	"os"
	"path/filepath"

	configutils "github.com/inference-gateway/cli/config/utils"
)

const (
	SandboxFileName    = "sandbox.yaml"
	DefaultSandboxPath = ConfigDirName + "/" + SandboxFileName
)

// SandboxConfig is the sandbox policy: the directories tools may touch and the
// paths they never may. It lives in its own sandbox.yaml so the policy can be
// reviewed on its own and the agent's file tools can never edit it.
type SandboxConfig struct {
	Directories    []string `yaml:"directories" mapstructure:"directories"`
	ProtectedPaths []string `yaml:"protected_paths" mapstructure:"protected_paths"`
}

// DefaultSandboxConfig allows the working directory and /tmp and protects the
// config dir, git metadata and the usual credential files.
func DefaultSandboxConfig() *SandboxConfig {
	return &SandboxConfig{
		Directories: []string{".", "/tmp"},
		ProtectedPaths: []string{
			ConfigDirName + "/",
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
		},
	}
}

// LoadSandbox reads sandbox.yaml over the defaults, so a file that only sets
// directories keeps the default protected paths. A missing file is the defaults.
func LoadSandbox(path string) (*SandboxConfig, error) {
	return configutils.LoadYAMLMerged(path, "sandbox", DefaultSandboxConfig)
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
