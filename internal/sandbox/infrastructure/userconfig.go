package sandboxinfra

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// PersistGrant puts an approved grant in front of the allowed entries of the
// userspace ~/.infer/sandbox.yaml, so it wins over a narrower entry next time.
// A grant inside a config dir is refused and stays session-only, so no future
// session's configuration becomes writable without a prompt.
func PersistGrant(grant sandboxdomain.Allowed) error {
	if inConfigDir(config.UserSpaceConfigDir(), grant.Path) || inConfigDir(config.ConfigDirName, grant.Path) {
		return errors.New("a grant inside the config dir stays session-only")
	}
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

// inConfigDir reports whether path is dir itself or lives beneath it.
func inConfigDir(dir, path string) bool {
	rel, err := filepath.Rel(config.CanonicalPath(dir), config.CanonicalPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
