package sandbox

import (
	"os"
	"path/filepath"
	"slices"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// WritePaths lists the directories a bash child may write while the kernel
// confines it: the writable allowed entries and grants, the carve-outs the file
// tools may write, the temp and tool cache dirs a build needs, and /dev. In plan
// mode the project gives way to the scratchpad, as ValidateWrite does.
func WritePaths(cfg *config.Config, mode agentdomain.AgentMode) []string {
	var paths []string
	if mode == agentdomain.AgentModePlan {
		paths = append(paths, config.ProjectScratchpadDir())
	} else {
		paths = append(paths, writableAllowed(cfg)...)
		paths = append(paths, WritableCarveOuts(cfg)...)
	}
	paths = append(paths, buildCacheDirs()...)
	paths = append(paths, os.TempDir(), "/dev")
	return absoluteDirs(paths)
}

func writableAllowed(cfg *config.Config) []string {
	var dirs []string
	for _, entry := range cfg.Tools.Sandbox.Filesystem.Allowed {
		if entry.Access.Allows(sandboxdomain.AccessWrite) && IsAnchored(entry.Path) {
			dirs = append(dirs, anchoredPath(entry.Path))
		}
	}
	for _, grant := range Granted.List() {
		if grant.Access.Allows(sandboxdomain.AccessWrite) {
			dirs = append(dirs, grant.Path)
		}
	}
	return dirs
}

// WritableCarveOuts lists the built-in directories the file tools may write
// although the config dirs ask: skills, plugins, runtime output, plans,
// insights and memory.
func WritableCarveOuts(cfg *config.Config) []string {
	var dirs []string
	if cfg.Agent.Skills.Enabled {
		dirs = append(dirs, skillsDirs()...)
	}
	if cfg.Plugins.Enabled {
		if dir, err := cfg.Plugins.ResolveDir(); err == nil {
			dirs = append(dirs, dir)
		}
	}
	dirs = append(dirs, runtimeDirs()...)
	dirs = append(dirs, configSubdirs(cfg, "plans", "projects.yaml")...)
	dirs = append(dirs, config.InsightsDir())
	if dir, ok := memoryDir(cfg.Memory); ok {
		dirs = append(dirs, dir)
	}
	return dirs
}

// buildCacheDirs are the caches build tools write without being asked: the
// user cache dir, the Go build and module caches, the cargo registry and git
// checkouts, and npm's cache. None holds credentials.
func buildCacheDirs() []string {
	var dirs []string
	if dir, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, dir)
	}
	if dir := os.Getenv("GOCACHE"); dir != "" {
		dirs = append(dirs, dir)
	}
	if dir := goModCache(); dir != "" {
		dirs = append(dirs, dir)
	}
	if home := cargoHome(); home != "" {
		dirs = append(dirs, filepath.Join(home, "registry"), filepath.Join(home, "git"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".npm"))
	}
	return dirs
}

func absoluteDirs(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err == nil && !slices.Contains(out, abs) {
			out = append(out, abs)
		}
	}
	return out
}
