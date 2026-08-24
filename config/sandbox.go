package config

import (
	"fmt"
	"regexp"
	"slices"
	"sync"
)

// SandboxPathError is returned when a path is denied by the sandbox
// allow-list, so callers can offer to extend the sandbox instead of just
// failing the tool call.
type SandboxPathError struct{ Path string }

func (e *SandboxPathError) Error() string {
	return fmt.Sprintf("path '%s' is outside configured sandbox directories", e.Path)
}

// Runtime sandbox grants: directories the user approved after a denial.
// ponytail: process-wide state like the port registry below it; fold into
// Config if one process ever runs multiple configs.
var (
	sandboxGrantsMu sync.RWMutex
	sandboxGrants   []string
)

// AddSandboxDirectory grants dir as an additional sandbox directory for the
// rest of the process. Idempotent.
func AddSandboxDirectory(dir string) {
	sandboxGrantsMu.Lock()
	defer sandboxGrantsMu.Unlock()
	if !slices.Contains(sandboxGrants, dir) {
		sandboxGrants = append(sandboxGrants, dir)
	}
}

func grantedSandboxDirectories() []string {
	sandboxGrantsMu.RLock()
	defer sandboxGrantsMu.RUnlock()
	return slices.Clone(sandboxGrants)
}

var sandboxDeniedRe = regexp.MustCompile(`path '(.+)' is outside configured sandbox directories`)

// SandboxDeniedPath extracts the denied path from a SandboxPathError message
// that has been flattened into a tool-result error string.
func SandboxDeniedPath(msg string) (string, bool) {
	m := sandboxDeniedRe.FindStringSubmatch(msg)
	if m == nil {
		return "", false
	}
	return m[1], true
}
