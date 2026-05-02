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

	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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
// is gated by config.AgentSkillsConfig — when disabled the scan is skipped
// entirely so there is zero token / IO cost.
type Service struct {
	cfg    *config.Config
	scopes []scopedDir
	mu     sync.RWMutex
	skills []domain.Skill
	errs   []domain.SkillLoadError
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
func (s *Service) Load(_ context.Context) error {
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
		scopes = searchScopes()
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
			skill, loadErr := loadSkill(skillDir, entry.Name(), scope.scope)
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

	return nil
}

// List returns a defensive copy of the loaded skills.
func (s *Service) List() []domain.Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Skill, len(s.skills))
	copy(out, s.skills)
	return out
}

// Errors returns a defensive copy of validation failures from the last Load.
func (s *Service) Errors() []domain.SkillLoadError {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.SkillLoadError, len(s.errs))
	copy(out, s.errs)
	return out
}

type scopedDir struct {
	dir   string
	scope domain.SkillScope
}

// searchScopes returns project-first then user-global. Project precedence is
// implemented by the caller via the `seen` map.
func searchScopes() []scopedDir {
	scopes := []scopedDir{
		{dir: filepath.Join(config.ConfigDirName, skillsSubdir), scope: domain.SkillScopeProject},
	}
	if home, err := os.UserHomeDir(); err == nil {
		scopes = append(scopes, scopedDir{
			dir:   filepath.Join(home, config.ConfigDirName, skillsSubdir),
			scope: domain.SkillScopeUser,
		})
	}
	return scopes
}

// loadSkill reads <skillDir>/SKILL.md, parses frontmatter, validates the
// fields, and returns the populated domain.Skill. Returns (nil, nil) when
// the directory has no SKILL.md (silently skipped — not an error). Returns
// (nil, err) when SKILL.md is present but invalid.
func loadSkill(skillDir, dirName string, scope domain.SkillScope) (*domain.Skill, *domain.SkillLoadError) {
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
		return nil, &domain.SkillLoadError{Path: absPath, Reason: fmt.Sprintf("read failed: %v", err)}
	}

	fm, parseErr := parseFrontmatter(data)
	if parseErr != nil {
		return nil, &domain.SkillLoadError{Path: absPath, Reason: parseErr.Error()}
	}

	if validationErr := validate(fm, dirName); validationErr != nil {
		return nil, &domain.SkillLoadError{Path: absPath, Reason: validationErr.Error()}
	}

	return &domain.Skill{
		Name:        fm.Name,
		Description: fm.Description,
		Path:        absPath,
		Scope:       scope,
	}, nil
}

// parseFrontmatter extracts the YAML block delimited by `---` lines at the
// top of the file. The body after the second delimiter is discarded — we
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
		return fm, fmt.Errorf("invalid YAML in frontmatter: %v", err)
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
