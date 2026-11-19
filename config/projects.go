package config

import (
	"fmt"
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

// VoiceSamplesDir is the user's voice samples library: the WAV recordings
// TextToSpeech clones a voice from, by bare file name.
func VoiceSamplesDir() string {
	return filepath.Join(UserSpaceConfigDir(), "voice", "samples")
}

// ConfigLookupDirs returns the config dirs that directory-shaped assets such as
// shortcuts/ are read from, userspace first, then the project, so later entries
// overlay earlier ones by name. ResolveConfigDir picks a single dir, which
// would hide everything `infer init` seeded into ~/.infer/shortcuts/.
func ConfigLookupDirs() []string {
	dirs := []string{UserSpaceConfigDir()}
	if _, err := os.Stat(ConfigDirName); err != nil {
		return dirs
	}
	abs, err := filepath.Abs(ConfigDirName)
	if err != nil || abs == dirs[0] {
		return dirs
	}
	return append(dirs, ConfigDirName)
}

// projectRuntimeSlug is the ProjectSlug of the working directory, or "default"
// when the cwd cannot be resolved.
func projectRuntimeSlug() string {
	cwd, err := os.Getwd()
	if err != nil || strings.TrimSpace(cwd) == "" {
		return "default"
	}
	return ProjectSlug(cwd)
}

// ProjectSlug maps a project's absolute directory to the flat directory name
// its runtime and conversation store use: /home/alice/repo becomes
// -home-alice-repo. A directory inside the userspace config dir is named by its
// path relative to it.
func ProjectSlug(dir string) string {
	if rel, err := filepath.Rel(UserSpaceConfigDir(), dir); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return slugOfPath(rel)
	}
	return slugOfPath(dir)
}

// slugOfPath joins the path's segments with "-", dropping the dot a hidden
// segment starts with, so /home/alice/.work/repo becomes -home-alice-work-repo.
func slugOfPath(path string) string {
	segments := strings.Split(path, string(filepath.Separator))
	for i, segment := range segments {
		segments[i] = strings.TrimPrefix(segment, ".")
	}
	return strings.Join(segments, "-")
}

// ProjectRuntimeDir is the per-project runtime root, ~/.infer/projects/<slug>.
// Runtime artifacts such as history, backups and exports are process output,
// not project configuration, so their defaults resolve here. It is resolved at
// call time like the sandbox paths, and an explicit config path always wins.
func ProjectRuntimeDir() string {
	return filepath.Join(UserSpaceConfigDir(), ProjectsDirName, projectRuntimeSlug())
}

// ProjectTmpDir is the per-project tmp scratch directory: the default target
// for chunked writes, dynamic skills, clipboard images and channel images.
func ProjectTmpDir() string {
	return filepath.Join(ProjectRuntimeDir(), "tmp")
}

// ProjectScratchpadDir is the per-project scratchpad: the one directory plan
// mode allows the file tools and Bash to write, so plans can be verified
// without touching the working tree.
func ProjectScratchpadDir() string {
	return filepath.Join(ProjectTmpDir(), "scratchpad")
}

// MediaDir returns the default directory for one kind of generated or retained
// media (sfx, video, recordings, ...). It is <project tmp>/media/<kind> while a
// project is open, else ~/.infer/tmp/media/<kind>.
func MediaDir(kind string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	if noProjectOpen(home) {
		return filepath.Join(home, ConfigDirName, "tmp", "media", kind), nil
	}
	return filepath.Join(ProjectTmpDir(), "media", kind), nil
}

// noProjectOpen reports whether the process runs outside any project: in
// $HOME, in the desktop's no-project ~/.infer/workspace, or in an unresolvable cwd.
func noProjectOpen(home string) bool {
	cwd, err := os.Getwd()
	return err != nil || cwd == home || cwd == filepath.Join(home, ConfigDirName, "workspace")
}
