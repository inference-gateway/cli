// Package skills implements discovery of Agent Skills. A skill is a folder
// containing a SKILL.md whose YAML frontmatter (name, description) is parsed
// at startup; the body is read lazily by the model via the Read tool.
//
// The on-disk format intentionally matches the contract shared by the standard,
// so a folder authored for any of them drops in unchanged.
package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"

	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

const (
	skillEntryFile      = "SKILL.md"
	skillNameMaxLen     = 64
	skillDescMaxLen     = 1024
	skillsSubdir        = "skills"
	frontmatterDelim    = "---"
	frontmatterMinParts = 3
)

// nameRegex enforces the lowercase-letters/digits/hyphens charset shared by
// the official spec.
var nameRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// frontmatter is intentionally permissive: only required keys are unmarshalled,
// unknown keys are silently dropped (forward-compat with vendor extensions
// like Gemini's `disabled:` flag).
type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// Service discovers SKILL.md files and exposes the parsed metadata. Loading
// is gated by config.AgentSkillsConfig - when disabled the scan is skipped
// entirely so there is zero token / IO cost.
type Service struct {
	cfg          *config.Config
	scopes       []scopedDir
	mu           sync.RWMutex
	skills       []agentdomain.Skill
	errs         []agentdomain.SkillLoadError
	catalog      *CatalogClient
	dynamicNames []string
}

// New returns a Service bound to cfg. Call Load to populate the skill list.
func New(cfg *config.Config) *Service {
	return &Service{cfg: cfg}
}

// newWithScopes returns a Service with explicit search paths. Used by tests
// to point at temporary directories without touching the real filesystem.
func newWithScopes(cfg *config.Config, scopes []scopedDir) *Service {
	return &Service{cfg: cfg, scopes: scopes}
}

// Load scans both project and user-global skill directories, parses
// frontmatter, validates each skill, and populates the in-memory list.
// When skills are disabled in config the call is a no-op and returns nil.
func (s *Service) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.skills = nil
	s.errs = nil

	if s.cfg == nil || !s.cfg.Agent.Skills.Enabled {
		return nil
	}

	disabled := make(map[string]struct{}, len(s.cfg.Agent.Skills.DisabledSkills))
	for _, name := range s.cfg.Agent.Skills.DisabledSkills {
		disabled[name] = struct{}{}
	}

	seen := make(map[string]struct{})

	scopes := s.scopes
	if scopes == nil {
		scopes = s.searchScopes()
	}

	for _, scope := range scopes {
		entries, err := os.ReadDir(scope.dir)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Warn("failed to read skills directory", "path", scope.dir, "error", err)
			}
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(scope.dir, entry.Name())
			skill, loadErr := LoadSkillMetadata(skillDir, entry.Name(), scope.scope, scope.pluginName)
			if loadErr != nil {
				logger.Warn("skipping invalid skill", "path", skillDir, "reason", loadErr.Reason)
				s.errs = append(s.errs, *loadErr)
				continue
			}
			if skill == nil {
				continue
			}
			if _, dropped := disabled[skill.Name]; dropped {
				continue
			}
			if _, dup := seen[skill.Name]; dup {
				logger.Debug("skill name already loaded from higher-priority scope, skipping", "name", skill.Name, "path", skillDir)
				continue
			}
			seen[skill.Name] = struct{}{}
			s.skills = append(s.skills, *skill)
		}
	}

	s.mergeCatalog(ctx, disabled)

	return nil
}

// mergeCatalog appends the centralized catalog's index entries to the loaded
// list as metadata-only skills (empty Path = body not downloaded yet); the
// SKILL.md is fetched on activation by Discover. Local skills win on name
// collision. Best-effort: a failed fetch just leaves the list local-only.
// Caller holds s.mu.
func (s *Service) mergeCatalog(ctx context.Context, disabled map[string]struct{}) {
	if !s.cfg.Agent.Skills.Discovery.Enabled {
		return
	}

	if s.catalog == nil {
		s.catalog = NewCatalogClient(s.cfg)
	}

	ctx, cancel := context.WithTimeout(ctx, catalogIndexTimeout)
	defer cancel()

	local := make(map[string]struct{}, len(s.skills))
	for _, sk := range s.skills {
		local[sk.Name] = struct{}{}
	}

	for _, entry := range s.catalog.Index(ctx) {
		if _, dup := local[entry.Name]; dup {
			continue
		}
		if _, dropped := disabled[entry.Name]; dropped {
			continue
		}
		local[entry.Name] = struct{}{}
		s.skills = append(s.skills, skillFromEntry(entry))
	}
}

// List returns a defensive copy of the loaded skills.
func (s *Service) List() []agentdomain.Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]agentdomain.Skill, len(s.skills))
	copy(out, s.skills)
	return out
}

// Get returns the loaded skill with the given name. Lookup is exact (names are
// validated to the lowercase `[a-z0-9-]+` charset at load time).
func (s *Service) Get(name string) (agentdomain.Skill, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sk := range s.skills {
		if sk.Name == name {
			return sk, true
		}
	}
	return agentdomain.Skill{}, false
}

// Errors returns a defensive copy of validation failures from the last Load.
func (s *Service) Errors() []agentdomain.SkillLoadError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]agentdomain.SkillLoadError, len(s.errs))
	copy(out, s.errs)
	return out
}

// Discover resolves a skill by name, downloading its body from the
// centralized catalog when the loaded entry is a metadata-only catalog
// placeholder (empty Path, seeded by Load). Any skill already backed by a
// file on disk - local or previously downloaded - is returned as-is, so this
// is the safe replacement for Get on the activation path. Returns false when
// the name is unknown, or when it is only resolvable via the catalog and
// discovery is disabled.
func (s *Service) Discover(ctx context.Context, name string) (agentdomain.Skill, bool) {
	known, ok := s.Get(name)
	if ok && known.Path != "" {
		return known, true
	}

	if s.cfg == nil || !s.cfg.Agent.Skills.Discovery.Enabled {
		return agentdomain.Skill{}, false
	}

	if s.catalog == nil {
		s.catalog = NewCatalogClient(s.cfg)
	}

	description := known.Description
	if !ok {
		entry, found := s.catalog.Lookup(ctx, name)
		if !found {
			return agentdomain.Skill{}, false
		}
		description = entry.Description
	}

	skillPath, err := s.catalog.DownloadSkill(ctx, name)
	if err != nil {
		logger.Warn("failed to download skill from catalog", "name", name, "error", err)
		return agentdomain.Skill{}, false
	}

	skill := agentdomain.Skill{
		Name:        name,
		Description: description,
		Path:        skillPath,
		Scope:       agentdomain.SkillScopeCatalog,
	}

	s.mu.Lock()
	if i := s.indexOf(name); i >= 0 {
		s.skills[i] = skill
	} else {
		s.skills = append(s.skills, skill)
	}
	s.dynamicNames = append(s.dynamicNames, name)
	s.mu.Unlock()

	return skill, true
}

// indexOf returns the position of name in s.skills, or -1. Caller holds s.mu.
func (s *Service) indexOf(name string) int {
	for i, sk := range s.skills {
		if sk.Name == name {
			return i
		}
	}
	return -1
}

// CleanupDynamic removes dynamically downloaded skills from disk.
// When cleanup is enabled in config, this removes all skills that were
// fetched from the catalog during this session. No-op when discovery
// is disabled or cleanup is explicitly turned off.
func (s *Service) CleanupDynamic(_ context.Context) error {
	if s.cfg == nil || !s.cfg.Agent.Skills.Discovery.Enabled {
		return nil
	}
	if s.cfg.Agent.Skills.Discovery.Cleanup != nil && !*s.cfg.Agent.Skills.Discovery.Cleanup {
		return nil
	}
	if len(s.dynamicNames) == 0 {
		return nil
	}
	return CleanupDynamic()
}

type scopedDir struct {
	dir        string
	scope      agentdomain.SkillScope
	pluginName string
}

// searchScopes returns the skill directories in precedence order: project,
// .agents/skills, user-global, then each enabled plugin's skills dir. The
// caller's `seen` map makes the first match win on name collision.
func (s *Service) searchScopes() []scopedDir {
	scopes := []scopedDir{
		{dir: filepath.Join(config.ConfigDirName, skillsSubdir), scope: agentdomain.SkillScopeProject},
		{dir: filepath.Join(config.AgentsDirName, skillsSubdir), scope: agentdomain.SkillScopeAgents},
	}
	if home, err := os.UserHomeDir(); err == nil {
		scopes = append(scopes, scopedDir{
			dir:   filepath.Join(home, config.ConfigDirName, skillsSubdir),
			scope: agentdomain.SkillScopeUser,
		})
	}
	if s.cfg != nil {
		for _, p := range s.cfg.Plugins.EnabledEntries() {
			dir, err := s.cfg.Plugins.PluginSkillsDir(p.Name)
			if err != nil {
				continue
			}
			scopes = append(scopes, scopedDir{dir: dir, scope: agentdomain.SkillScopePlugin, pluginName: p.Name})
		}
	}
	return scopes
}

// LoadSkillMetadata reads <skillDir>/SKILL.md, parses frontmatter, validates
// the fields, and returns the populated agentdomain.Skill. Returns (nil, nil) when
// the directory has no SKILL.md, (nil, err) when SKILL.md is invalid.
// pluginName is set only for plugin-scoped skills.
func LoadSkillMetadata(skillDir, dirName string, scope agentdomain.SkillScope, pluginName string) (*agentdomain.Skill, *agentdomain.SkillLoadError) {
	entryPath := filepath.Join(skillDir, skillEntryFile)
	absPath, absErr := filepath.Abs(entryPath)
	if absErr != nil {
		absPath = entryPath
	}

	data, err := os.ReadFile(entryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &agentdomain.SkillLoadError{Path: absPath, Reason: fmt.Sprintf("read failed: %v", err)}
	}

	fm, parseErr := parseFrontmatter(data)
	if parseErr != nil {
		return nil, &agentdomain.SkillLoadError{Path: absPath, Reason: parseErr.Error()}
	}

	if validationErr := validate(fm, dirName); validationErr != nil {
		return nil, &agentdomain.SkillLoadError{Path: absPath, Reason: validationErr.Error()}
	}

	return &agentdomain.Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Path:        absPath,
		Scope:       scope,
		PluginName:  pluginName,
	}, nil
}

// parseFrontmatter extracts the YAML block delimited by `---` lines at the
// top of the file. The body after the second delimiter is discarded - we
// only care about metadata at startup.
func parseFrontmatter(data []byte) (frontmatter, error) {
	var fm frontmatter

	content := strings.TrimLeft(string(data), "\ufeff \t\r\n")
	if !strings.HasPrefix(content, frontmatterDelim) {
		return fm, fmt.Errorf("missing YAML frontmatter (expected `---` at top of file)")
	}

	parts := strings.SplitN(content, frontmatterDelim, frontmatterMinParts)
	if len(parts) < frontmatterMinParts {
		return fm, fmt.Errorf("malformed frontmatter (expected closing `---` delimiter)")
	}

	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return fm, fmt.Errorf("invalid YAML in frontmatter: %w", err)
	}

	return fm, nil
}

// validate enforces the official spec rules.
func validate(fm frontmatter, dirName string) error {
	if fm.Name == "" {
		return fmt.Errorf("`name` is required in frontmatter")
	}
	if len(fm.Name) > skillNameMaxLen {
		return fmt.Errorf("`name` is %d chars (max %d)", len(fm.Name), skillNameMaxLen)
	}
	if !nameRegex.MatchString(fm.Name) {
		return fmt.Errorf("`name` must match %s (lowercase letters, digits, hyphens)", nameRegex.String())
	}
	if fm.Name != dirName {
		return fmt.Errorf("`name` (%s) must equal the directory name (%s)", fm.Name, dirName)
	}
	if fm.Description == "" {
		return fmt.Errorf("`description` is required in frontmatter")
	}
	if len(fm.Description) > skillDescMaxLen {
		return fmt.Errorf("`description` is %d chars (max %d)", len(fm.Description), skillDescMaxLen)
	}

	return nil
}
