package sandbox

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

// ValidateRead checks that a path and the file it resolves to through
// symlinks may both be read. Checking the target stops a link inside the
// sandbox from reaching a file outside it.
func ValidateRead(cfg *config.Config, path string) error {
	return validate(cfg, path, sandboxdomain.AccessRead)
}

// ValidateWrite is ValidateRead for writes. The userspace policy files and
// the custom-tool directories are never writable, whatever the rules say.
func ValidateWrite(cfg *config.Config, path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	if label, ok := protectedPolicyPath(absPath); ok {
		return fmt.Errorf("path '%s' is the %s policy, which infer's file tools never edit", path, label)
	}
	if isWithinCustomToolsDir(cfg, absPath) {
		return fmt.Errorf("path '%s' is in a custom tools directory, which infer's file tools never edit", path)
	}
	return validate(cfg, path, sandboxdomain.AccessWrite)
}

// AllowedDirectories are the anchored allowed paths, in order, for callers
// that want somewhere sensible to look.
func AllowedDirectories(cfg *config.Config) []string {
	var dirs []string
	for _, entry := range cfg.Tools.Sandbox.Filesystem.Allowed {
		if IsAnchored(entry.Path) {
			dirs = append(dirs, anchoredPath(entry.Path))
		}
	}
	return dirs
}

func validate(cfg *config.Config, path string, access sandboxdomain.Access) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	if err := check(cfg, path, absPath, access); err != nil {
		return err
	}
	if target := config.RealPath(absPath); target != absPath {
		return check(cfg, target, target, access)
	}
	return nil
}

// check decides one spelling of a path: denied entries win, a blocking one over
// one that asks, then the built-in carve-outs, then the first matching allowed
// entry, and anything else asks the user. An empty allowed list is no boundary.
// A grant only unlocks what approval could have, so it still respects denied.
func check(cfg *config.Config, path, absPath string, access sandboxdomain.Access) error {
	carveOut, inCarveOut := implicitAccess(cfg, absPath)

	approvalRule := ""
	for _, entry := range cfg.Tools.Sandbox.Filesystem.Denied {
		if inCarveOut && isConfigDirRule(entry.Path) {
			continue
		}
		if !matches(entry.Path, path, absPath) {
			continue
		}
		if entry.OnViolation != sandboxdomain.ViolationApproval {
			return fmt.Errorf("access to path '%s' is excluded for security", path)
		}
		approvalRule = cmp.Or(approvalRule, entry.Path)
	}
	if approvalRule != "" {
		if granted(absPath, access) {
			return nil
		}
		return &sandboxdomain.DeniedError{Path: path, Access: access, Rule: approvalRule}
	}

	if inCarveOut {
		if carveOut.Allows(access) {
			return nil
		}
		return fmt.Errorf("path '%s' is in a read-only library directory", path)
	}
	if len(cfg.Tools.Sandbox.Filesystem.Allowed) == 0 {
		return nil
	}
	for _, entry := range cfg.Tools.Sandbox.Filesystem.Allowed {
		if matches(entry.Path, path, absPath) {
			if entry.Access.Allows(access) {
				return nil
			}
			break
		}
	}
	if granted(absPath, access) {
		return nil
	}
	return &sandboxdomain.DeniedError{Path: path, Access: access}
}

// implicitAccess is the access the built-in carve-outs give absPath: the
// operational directories infer itself needs (skills, plugins, runtime output,
// plans, insights, memory) are writable, the Go library dirs are read-only.
func implicitAccess(cfg *config.Config, absPath string) (sandboxdomain.Access, bool) {
	switch {
	case cfg.Agent.Skills.Enabled && isWithinSkillsDir(absPath),
		cfg.Plugins.Enabled && isWithinPluginsDir(cfg, absPath),
		isWithinRuntimeDirs(absPath),
		isWithinConfigSubdir(cfg, absPath, "plans", "projects.yaml"),
		isWithinDir(absPath, config.InsightsDir()),
		isWithinMemoryDir(absPath, cfg.Memory):
		return sandboxdomain.AccessWrite, true
	case isWithinGoLibDirs(absPath):
		return sandboxdomain.AccessRead, true
	}
	return "", false
}

// isConfigDirRule reports whether a denied rule names a config directory
// itself, in any spelling (.infer, ./.infer/, .infer/*, ~/.infer), so the
// built-in carve-outs inside it stay open.
func isConfigDirRule(rulePath string) bool {
	return filepath.Base(strings.TrimSuffix(rulePath, "/*")) == config.ConfigDirName
}

// Grants are the entries the user approved after a denial. They extend the
// configured policy for the rest of the process and never feed the system
// prompt, which must stay byte-stable within a session.
type Grants struct {
	mu      sync.RWMutex
	entries []sandboxdomain.Allowed
}

// Granted holds the process-wide grants.
// ponytail: process-wide like the port registry, fold into an injected guard if
// one process ever runs multiple configs.
var Granted Grants

// Add grants entry for the rest of the process. Idempotent.
func (g *Grants) Add(entry sandboxdomain.Allowed) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !slices.Contains(g.entries, entry) {
		g.entries = append(g.entries, entry)
	}
}

// List returns a copy of the granted entries.
func (g *Grants) List() []sandboxdomain.Allowed {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return slices.Clone(g.entries)
}

func granted(absPath string, access sandboxdomain.Access) bool {
	for _, grant := range Granted.List() {
		if grant.Access.Allows(access) && isWithinDir(absPath, grant.Path) {
			return true
		}
	}
	return false
}

// GrantFor is the entry worth asking the user for after a denial: the exact
// path when a denied entry matched or it sits in a config dir, otherwise the
// directory around it so the agent can keep working there without a prompt
// per file.
func GrantFor(cfg *config.Config, denial *sandboxdomain.DeniedError) sandboxdomain.Allowed {
	path := denial.Path
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if dir := grantDir(path); denial.Rule == "" && !isWithinConfigDirs(cfg, dir) {
		return sandboxdomain.Allowed{Path: dir, Access: denial.Access}
	}
	return sandboxdomain.Allowed{Path: path, Access: denial.Access}
}

// grantDir is absPath when it is an existing directory, otherwise its parent.
func grantDir(absPath string) string {
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		return absPath
	}
	return filepath.Dir(absPath)
}

// isWithinConfigDirs reports whether path sits inside a config dir: the
// userspace one, or the one the config was resolved from, whose files may only
// ever be granted file by file.
func isWithinConfigDirs(cfg *config.Config, path string) bool {
	dirs := []string{config.UserSpaceConfigDir()}
	if cfg != nil {
		dirs = append(dirs, cfg.GetConfigDir())
	}
	for _, dir := range dirs {
		if isWithinDir(path, dir) {
			return true
		}
	}
	return false
}

// matches reports whether rulePath covers the path. An anchored rule covers the
// file or directory it names and everything beneath it. A relative pattern is
// matched the way denied patterns always were: dir/ at any depth, *glob on the
// base name, or an exact name or suffix.
func matches(rulePath, path, absPath string) bool {
	if IsAnchored(rulePath) {
		return isWithinDir(absPath, anchoredPath(rulePath))
	}
	return matchesPattern(rulePath, path)
}

func anchoredPath(rulePath string) string {
	if expanded, ok := expandTilde(rulePath); ok {
		return expanded
	}
	return rulePath
}

func IsAnchored(rulePath string) bool {
	return filepath.IsAbs(rulePath) || rulePath == "." || rulePath == ".." ||
		strings.HasPrefix(rulePath, "./") || strings.HasPrefix(rulePath, "../") ||
		rulePath == "~" || strings.HasPrefix(rulePath, "~/")
}

// protectedPolicyFiles are the userspace policy files the file tools never
// write, whatever the rules say, so the agent can never widen its own
// sandbox or lower its own approval bar.
var protectedPolicyFiles = []struct {
	label string
	path  func() (string, error)
}{
	{label: "sandbox", path: config.UserSandboxPath},
	{label: "tools", path: config.UserToolsPath},
}

// protectedPolicyPath reports the label of the protected policy file absPath
// resolves to, if any.
func protectedPolicyPath(absPath string) (string, bool) {
	target := config.CanonicalPath(absPath)
	for _, policy := range protectedPolicyFiles {
		file, err := policy.path()
		if err != nil {
			continue
		}
		if target == config.CanonicalPath(file) {
			return policy.label, true
		}
	}
	return "", false
}

// isWithinCustomToolsDir reports whether absPath is inside a directory custom
// tools load from. It resolves symlinks and ignores case, so neither a link
// nor another spelling on a case-insensitive filesystem reaches one.
func isWithinCustomToolsDir(cfg *config.Config, absPath string) bool {
	path := config.CanonicalPath(absPath)
	for _, dir := range append(config.ProjectToolsDirs(), cfg.CustomToolsDir()) {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if isBeneath(path, config.CanonicalPath(absDir)) {
			return true
		}
	}
	return false
}

// isWithinSkillsDir reports whether absPath lives inside a skills directory:
// ./.infer/skills, ./.agents/skills or ~/.infer/skills. The carve-out lets
// skills load although the config dirs ask, and *.env stays denied.
func isWithinSkillsDir(absPath string) bool {
	dirs := []string{filepath.Join(config.ConfigDirName, "skills"), filepath.Join(config.AgentsDirName, "skills")}
	if homeDir, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(homeDir, config.ConfigDirName, "skills"))
	}

	for _, dir := range dirs {
		if isWithinDir(absPath, dir) {
			return true
		}
	}
	return false
}

// isWithinDir reports whether absPath is dir itself or lives beneath it. dir may
// be relative, and it matches both as written and with its symlinks resolved, so
// a resolved path still lands inside a symlinked dir such as /tmp on macOS.
func isWithinDir(absPath, dir string) bool {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	return isBeneath(absPath, absDir) || isBeneath(absPath, config.RealPath(absDir))
}

func isBeneath(absPath, absDir string) bool {
	rel, err := filepath.Rel(absDir, absPath)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// isWithinRuntimeDirs reports whether absPath lives inside a runtime output
// dir: the artifact subdirs of ~/.infer/projects/<project-slug> or the
// machine-scoped ones under ~/.infer. The agent reads and writes those
// although the rest of the config dirs ask.
func isWithinRuntimeDirs(absPath string) bool {
	runtimeRoot := config.ProjectRuntimeDir()
	for _, name := range config.RuntimeArtifactDirNames {
		if isWithinDir(absPath, filepath.Join(runtimeRoot, name)) {
			return true
		}
	}

	userSpace := config.UserSpaceConfigDir()
	for _, name := range config.UserspaceRuntimeDirNames {
		if isWithinDir(absPath, filepath.Join(userSpace, name)) {
			return true
		}
	}
	return false
}

// isWithinConfigSubdir reports whether absPath lives inside one of the named
// entries of ./.infer or of the resolved config dir, so operational areas such
// as persisted plans and the desktop's projects.yaml stay reachable wherever
// the config was loaded from.
func isWithinConfigSubdir(cfg *config.Config, absPath string, names ...string) bool {
	configDirs := []string{config.ConfigDirName}
	if resolved := cfg.GetConfigDir(); resolved != "" && resolved != config.ConfigDirName {
		configDirs = append(configDirs, resolved)
	}

	for _, name := range names {
		for _, base := range configDirs {
			if isWithinDir(absPath, filepath.Join(base, name)) {
				return true
			}
		}
	}
	return false
}

// isWithinPluginsDir reports whether absPath lives inside the plugins
// storage root, so plugin SKILL.md bodies stay readable.
func isWithinPluginsDir(cfg *config.Config, absPath string) bool {
	dir, err := cfg.Plugins.ResolveDir()
	if err != nil {
		return false
	}
	return isWithinDir(absPath, dir)
}

// isWithinGoLibDirs reports whether absPath lives inside the Go module cache
// ($GOMODCACHE or $GOPATH/pkg/mod) or the standard library source
// ($GOROOT/src), resolved from the environment. The carve-out is read-only.
func isWithinGoLibDirs(absPath string) bool {
	gomodcache := os.Getenv("GOMODCACHE")
	if gomodcache == "" {
		gopath := os.Getenv("GOPATH")
		if gopath == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return false
			}
			gopath = filepath.Join(home, "go")
		}
		gomodcache = filepath.Join(gopath, "pkg", "mod")
	}
	if isWithinDir(absPath, gomodcache) {
		return true
	}

	goroot := os.Getenv("GOROOT")
	return goroot != "" && isWithinDir(absPath, filepath.Join(goroot, "src"))
}

// isWithinMemoryDir reports whether absPath lives inside the global memory
// directory (~/.infer/memory or the configured Memory.Dir override), so reads of
// memory fact-files succeed even though .infer/ is otherwise protected. Gated on
// Memory.Enabled. The Memory tool itself writes via its own atomic writer rather
// than the sandboxed file writer, so this carve-out mainly governs manual reads.
func isWithinMemoryDir(absPath string, m config.MemoryConfig) bool {
	if !m.Enabled {
		return false
	}
	dir := m.Dir
	if strings.TrimSpace(dir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, config.ConfigDirName, config.MemoryDirName)
	}
	return isWithinDir(absPath, dir)
}

// matchesPattern matches a relative pattern: dir/ at any depth, *glob on
// the base name (unless it is dir/*), or an exact name or path suffix.
func matchesPattern(pattern, path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))

	if strings.HasSuffix(pattern, "/") || strings.HasSuffix(pattern, "/*") {
		dir := strings.TrimSuffix(strings.TrimSuffix(pattern, "*"), "/")
		return strings.Contains(normalized, "/"+dir+"/") || strings.HasSuffix(normalized, "/"+dir) ||
			strings.HasPrefix(normalized, dir+"/") || normalized == dir
	}

	if strings.Contains(pattern, "*") {
		matched, err := filepath.Match(pattern, filepath.Base(normalized))
		return err == nil && matched
	}

	return normalized == pattern || strings.HasSuffix(normalized, "/"+pattern)
}
