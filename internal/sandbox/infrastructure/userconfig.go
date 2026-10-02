package sandboxinfra

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// PersistGrant adds an approved grant to the userspace ~/.infer/sandbox.yaml.
// A write grant goes first so it wins over a narrower read-only entry, a read
// grant goes last so it never shadows a write entry. A grant inside a config
// dir is refused and stays session-only.
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
	if grant.Access == sandboxdomain.AccessRead {
		sandboxCfg.Filesystem.Allowed = append(sandboxCfg.Filesystem.Allowed, grant)
	} else {
		sandboxCfg.Filesystem.Allowed = append([]sandboxdomain.Allowed{grant}, sandboxCfg.Filesystem.Allowed...)
	}
	return config.SaveSandbox(path, sandboxCfg)
}

// inConfigDir reports whether path is dir itself or lives beneath it.
func inConfigDir(dir, path string) bool {
	rel, err := filepath.Rel(config.CanonicalPath(dir), config.CanonicalPath(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
