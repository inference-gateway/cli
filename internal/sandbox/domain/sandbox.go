package sandboxdomain

import (
	"fmt"
	"regexp"

	yaml "gopkg.in/yaml.v3"
)

// ToolSandboxAccess names the synthetic approval the agent raises when a tool
// is denied a path, so the user can grant it instead of failing the call.
const ToolSandboxAccess = "SandboxAccess"

// Access is what a rule lets a tool do with a path. Write implies read.
type Access string

const (
	AccessRead  Access = "read"
	AccessWrite Access = "write"
)

// Allows reports whether a holder of a may perform want.
func (a Access) Allows(want Access) bool {
	return a == AccessWrite || a == want
}

// Valid reports whether a is one of the two access levels.
func (a Access) Valid() bool {
	return a == AccessRead || a == AccessWrite
}

// Violation is what happens when a path is denied: fail the call, or ask the
// user to grant the access for the session.
type Violation string

const (
	ViolationBlock    Violation = "block"
	ViolationApproval Violation = "approval"
)

// Valid reports whether v is a known violation behaviour. Empty means block.
func (v Violation) Valid() bool {
	return v == "" || v == ViolationBlock || v == ViolationApproval
}

// Allowed opens Path to the tools with Access (write when unset). Path is an
// anchored directory or file (absolute, ~, . or ./) or a relative pattern
// matched at any depth (dir/, *.glob, name). In YAML it is a string or a map.
type Allowed struct {
	Path   string `yaml:"path"`
	Access Access `yaml:"access,omitempty"`
}

// Denied closes Path whatever Allowed says. OnViolation decides whether a
// denied call fails or asks the user. In YAML it is a string or a map.
type Denied struct {
	Path        string    `yaml:"path"`
	OnViolation Violation `yaml:"on_violation,omitempty"`
}

// Allow opens each path for read and write.
func Allow(paths ...string) []Allowed {
	out := make([]Allowed, 0, len(paths))
	for _, p := range paths {
		out = append(out, Allowed{Path: p, Access: AccessWrite})
	}
	return out
}

// Deny closes each path and blocks on violation.
func Deny(paths ...string) []Denied {
	out := make([]Denied, 0, len(paths))
	for _, p := range paths {
		out = append(out, Denied{Path: p})
	}
	return out
}

func (a *Allowed) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		a.Path, a.Access = node.Value, AccessWrite
		return nil
	}
	type plain Allowed
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	*a = Allowed(v)
	if a.Access == "" {
		a.Access = AccessWrite
	}
	return nil
}

func (a Allowed) MarshalYAML() (any, error) {
	if a.Access == AccessWrite {
		return a.Path, nil
	}
	type plain Allowed
	return plain(a), nil
}

func (d *Denied) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		d.Path = node.Value
		return nil
	}
	type plain Denied
	var v plain
	if err := node.Decode(&v); err != nil {
		return err
	}
	*d = Denied(v)
	return nil
}

func (d Denied) MarshalYAML() (any, error) {
	if d.OnViolation == "" || d.OnViolation == ViolationBlock {
		return d.Path, nil
	}
	type plain Denied
	return plain(d), nil
}

// DeniedError is returned when a path is denied and the user may grant it.
// Rule names the denied entry, or is empty when the path is outside allowed.
type DeniedError struct {
	Path   string
	Access Access
	Rule   string
}

func (e *DeniedError) Error() string {
	if e.Rule == "" {
		return fmt.Sprintf("%s to path '%s' is denied by the sandbox", e.Access, e.Path)
	}
	return fmt.Sprintf("%s to path '%s' is denied by the sandbox (rule '%s')", e.Access, e.Path, e.Rule)
}

var deniedRe = regexp.MustCompile(`(read|write) to path '(.+?)' is denied by the sandbox(?: \(rule '(.+)'\))?`)

// ParseDenial recovers a DeniedError from a message that has been flattened
// into a tool-result error string.
func ParseDenial(msg string) (*DeniedError, bool) {
	m := deniedRe.FindStringSubmatch(msg)
	if m == nil {
		return nil, false
	}
	return &DeniedError{Path: m[2], Access: Access(m[1]), Rule: m[3]}, true
}
