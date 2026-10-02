package sandboxinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	config "github.com/inference-gateway/cli/config"
)

// PersistDirectory appends dir to the directories in the userspace
// ~/.infer/sandbox.yaml. current seeds a file that does not exist yet, so
// persisting never shrinks the effective allow-list.
func PersistDirectory(dir string, current []string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	path := filepath.Join(home, config.ConfigDirName, config.SandboxFileName)

	sandboxCfg, err := config.LoadSandbox(path)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		sandboxCfg.Directories = slices.Clone(current)
	}
	if slices.Contains(sandboxCfg.Directories, dir) {
		return nil
	}
	sandboxCfg.Directories = append(sandboxCfg.Directories, dir)
	return config.SaveSandbox(path, sandboxCfg)
}
