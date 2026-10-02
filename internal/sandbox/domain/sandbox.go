package sandboxdomain

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"
)

// ToolSandboxAccess names the synthetic approval the agent raises when a tool
// is denied a path, so the user can grant the directory instead of failing.
const ToolSandboxAccess = "SandboxAccess"

// DeniedError is returned when a path is outside the sandbox directories, so
// callers can offer to extend the sandbox instead of just failing the call.
type DeniedError struct{ Path string }

func (e *DeniedError) Error() string {
	return fmt.Sprintf("path '%s' is outside configured sandbox directories", e.Path)
}

var deniedRe = regexp.MustCompile(`path '(.+)' is outside configured sandbox directories`)

// DeniedPath extracts the denied path from a DeniedError message that has been
// flattened into a tool-result error string.
func DeniedPath(msg string) (string, bool) {
	m := deniedRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Grants are the directories the user approved after a denial. They extend the
// configured policy for the rest of the process and never feed the system
// prompt, which must stay byte-stable within a session.
type Grants struct {
	mu   sync.RWMutex
	dirs []string
}

// Granted holds the process-wide grants.
// ponytail: process-wide like the port registry, fold into an injected guard if
// one process ever runs multiple configs.
var Granted Grants

// Add grants dir for the rest of the process. Idempotent.
func (g *Grants) Add(dir string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !slices.Contains(g.dirs, dir) {
		g.dirs = append(g.dirs, dir)
	}
}

// List returns a copy of the granted directories.
func (g *Grants) List() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return slices.Clone(g.dirs)
}

// GrantDir maps a denied path to the directory worth granting: the path itself
// when it is an existing directory, otherwise its parent.
func GrantDir(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if info, err := os.Stat(abs); err == nil && info.IsDir() {
		return abs
	}
	return filepath.Dir(abs)
}
