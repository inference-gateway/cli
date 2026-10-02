package sandboxinfra

import (
	"slices"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// PersistGrant puts an approved grant in front of the allowed entries of the
// userspace ~/.infer/sandbox.yaml, so it wins over a narrower entry next time.
func PersistGrant(grant sandboxdomain.Allowed) error {
	path, err := config.UserSandboxPath()
	if err != nil {
		return err
	}
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
