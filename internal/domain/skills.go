package domain

import "context"

// SkillScope identifies whether a skill came from the project (.infer/skills/)
// or the user-global location (~/.infer/skills/). Used by the system-prompt
// injection and `infer skills list` to disambiguate same-name skills and to
// tell the user where each skill lives.
type SkillScope string

const (
	SkillScopeProject SkillScope = "project"
	SkillScopeUser    SkillScope = "user"
)

// Skill is the in-memory metadata for a discovered SKILL.md. The body of the
// file is intentionally not loaded at startup - only frontmatter - so the
// model reads it on demand via the existing Read tool (progressive
// disclosure, matching the contract).
type Skill struct {
	Name        string
	Description string
	Path        string
	Scope       SkillScope
}

// SkillLoadError records a per-skill validation failure so `infer skills
// list` can surface why a directory was skipped without crashing startup.
type SkillLoadError struct {
	Path   string
	Reason string
}

// SkillsService discovers and exposes Agent Skills. Implementations must be
// safe for concurrent reads after Load returns.
type SkillsService interface {
	// Load scans the configured skill directories and populates the
	// in-memory list. Safe to call once at startup; calling again rescans.
	// Returns nil and does nothing when skills are disabled in config.
	Load(ctx context.Context) error
	// List returns the currently loaded skills. The slice is a defensive
	// copy - callers may retain or mutate it freely.
	List() []Skill
	// Errors returns validation failures encountered during the most recent
	// Load. Cleared on each Load call.
	Errors() []SkillLoadError
}
