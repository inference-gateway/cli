package config

import (
	"os"
	"path/filepath"
	"strings"
)

// ProjectsDirName is the root of the per-project runtime layout under ~/.infer.
const ProjectsDirName = "projects"

// UserSpaceConfigDir returns the userspace config dir (~/.infer), falling back
// to the bare project-relative ConfigDirName when $HOME cannot be resolved
// (paths then land next to the working directory).
func UserSpaceConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ConfigDirName
	}
	return filepath.Join(home, ConfigDirName)
}

// projectRuntimeSlug maps the absolute working directory to a flat directory
// name, the scheme the conversation store uses: /home/alice/repo becomes
// -home-alice-repo. "default" is used when the cwd cannot be resolved.
func projectRuntimeSlug() string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return "default"
	}
	return strings.ReplaceAll(cwd, string(filepath.Separator), "-")
}

// ProjectRuntimeDir is the per-project runtime root, ~/.infer/projects/<project-slug>.
// Runtime artifacts (history, backups, tmp scratch, artifacts, exports) are
// process output rather than project configuration, so their defaults resolve
// here instead of into the project-local ./.infer directory. $HOME and the cwd
// (via the slug) are resolved at call time, matching how the sandbox validates
// paths; an explicit config path always wins over these defaults.
func ProjectRuntimeDir() string {
	return filepath.Join(UserSpaceConfigDir(), ProjectsDirName, projectRuntimeSlug())
}

// ProjectTmpDir is the per-project tmp scratch directory: the default target
// for chunked writes, dynamic skills, clipboard images, channel images, and
// screenshots.
func ProjectTmpDir() string {
	return filepath.Join(ProjectRuntimeDir(), "tmp")
}
