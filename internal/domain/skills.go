package domain

import "context"

// SkillScope identifies where a skill came from: the project (.infer/skills/),
// the open-standard location (.agents/skills/), the user-global location
// (~/.infer/skills/), or an installed plugin (~/.infer/plugins/<name>/skills/).
type SkillScope string

const (
	SkillScopeProject SkillScope = "project"
	SkillScopeAgents  SkillScope = "agents"
	SkillScopeUser    SkillScope = "user"
	SkillScopePlugin  SkillScope = "plugin"
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
	PluginName  string
}

// DisplayName returns the qualified name for display. Plugin skills are shown
// as "pluginName:skillName" so the user/LLM can reference them unambiguously.
func (s Skill) DisplayName() string {
	if s.Scope == SkillScopePlugin && s.PluginName != "" {
		return s.PluginName + ":" + s.Name
	}
	return s.Name
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
	// Get returns the loaded skill with the given name and true, or a zero
	// Skill and false when no such skill is loaded. Used by deterministic
	// activation to resolve an explicitly invoked skill name to its metadata
	// (description + path) for injection.
	Get(name string) (Skill, bool)
	// Errors returns validation failures encountered during the most recent
	// Load. Cleared on each Load call.
	Errors() []SkillLoadError
}
