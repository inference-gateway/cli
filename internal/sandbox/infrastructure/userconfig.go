package sandboxinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// PersistGrant puts an approved grant in front of the allowed entries of the
// userspace ~/.infer/sandbox.yaml, so it wins over a narrower entry next time.
func PersistGrant(grant sandboxdomain.Allowed) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory: %w", err)
	}
	path := filepath.Join(home, config.ConfigDirName, config.SandboxFileName)

	sandboxCfg, err := config.LoadSandbox(path)
	if err != nil {
		return err
	}
	if slices.Contains(sandboxCfg.Filesystem.Allowed, grant) {
		return nil
	}
	sandboxCfg.Filesystem.Allowed = append([]sandboxdomain.Allowed{grant}, sandboxCfg.Filesystem.Allowed...)
	return config.SaveSandbox(path, sandboxCfg)
}
