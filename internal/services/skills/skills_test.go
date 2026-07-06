package skills

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// writeSkill creates <baseDir>/<dirName>/SKILL.md with the given body. The
// body is written verbatim - callers control whether the frontmatter is
// valid. Returns the absolute path to the SKILL.md.
func writeSkill(t *testing.T, baseDir, dirName, body string) string {
	t.Helper()
	skillDir := filepath.Join(baseDir, dirName)
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	path := filepath.Join(skillDir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func validSkillBody(name, description string) string {
	return "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# Body\n"
}

// scope returns a single project-scope search dir pointing at root.
func scope(root string) []scopedDir {
	return []scopedDir{{dir: root, scope: domain.SkillScopeProject}}
}

func enabledCfg() *config.Config {
	return &config.Config{
		Agent: config.AgentConfig{
			Skills: config.AgentSkillsConfig{Enabled: true},
		},
	}
}

func TestLoad_Disabled_NoOp(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "valid-skill", validSkillBody("valid-skill", "A real skill that should not be loaded when disabled."))

	cfg := &config.Config{
		Agent: config.AgentConfig{
			Skills: config.AgentSkillsConfig{Enabled: false},
		},
	}
	s := newWithScopes(cfg, scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List(), "List() should be empty when skills are disabled")
	require.Empty(t, s.Errors(), "Errors() should be empty when skills are disabled")
}

func TestParse_ValidFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "pdf-helper", validSkillBody("pdf-helper", "Extract text from PDFs."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	got := s.List()
	require.Len(t, got, 1)
	require.Equal(t, "pdf-helper", got[0].Name)
	require.Equal(t, "Extract text from PDFs.", got[0].Description)
	require.Equal(t, domain.SkillScopeProject, got[0].Scope)
	require.True(t, filepath.IsAbs(got[0].Path), "path must be absolute, got %q", got[0].Path)
	require.True(t, strings.HasSuffix(got[0].Path, filepath.Join("pdf-helper", "SKILL.md")))
	require.Empty(t, s.Errors())
}

func TestParse_MissingName(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "no-name", "---\ndescription: No name field at all.\n---\n")

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "name")
}

func TestParse_MissingDescription(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "no-desc", "---\nname: no-desc\n---\n")

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "description")
}

func TestParse_NameTooLong(t *testing.T) {
	tmp := t.TempDir()
	longName := strings.Repeat("a", 65)
	writeSkill(t, tmp, longName, validSkillBody(longName, "Too long."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "max")
}

func TestParse_NameInvalidChars(t *testing.T) {
	tmp := t.TempDir()
	for _, badName := range []string{"With_Underscore", "UPPERCASE", "with space"} {
		sub := filepath.Join(tmp, "case-"+strings.ReplaceAll(badName, " ", "_"))
		require.NoError(t, os.MkdirAll(sub, 0o755))
		writeSkill(t, sub, badName, validSkillBody(badName, "Invalid charset."))

		s := newWithScopes(enabledCfg(), scope(sub))
		require.NoError(t, s.Load(context.Background()))
		require.Empty(t, s.List(), "expected no skills loaded for name %q", badName)
		require.Len(t, s.Errors(), 1)
	}
}

func TestParse_NameDoesNotMatchDir(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "foo", validSkillBody("bar", "Mismatched name."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "directory")
}

func TestParse_DescriptionTooLong(t *testing.T) {
	tmp := t.TempDir()
	longDesc := strings.Repeat("x", 1025)
	writeSkill(t, tmp, "long-desc", validSkillBody("long-desc", longDesc))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "max")
}

func TestParse_UnknownKeysTolerated(t *testing.T) {
	tmp := t.TempDir()
	body := "---\nname: extras\ndescription: Has extra keys.\ndisabled: false\nallowed-tools:\n  - Bash\n---\n\n# Body\n"
	writeSkill(t, tmp, "extras", body)

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Len(t, s.List(), 1)
	require.Equal(t, "extras", s.List()[0].Name)
	require.Empty(t, s.Errors())
}

func TestPrecedence_ProjectOverridesUser(t *testing.T) {
	projDir := t.TempDir()
	userDir := t.TempDir()

	writeSkill(t, projDir, "shared", validSkillBody("shared", "Project version."))
	writeSkill(t, userDir, "shared", validSkillBody("shared", "User version."))
	writeSkill(t, userDir, "user-only", validSkillBody("user-only", "Only in user scope."))

	s := newWithScopes(enabledCfg(), []scopedDir{
		{dir: projDir, scope: domain.SkillScopeProject},
		{dir: userDir, scope: domain.SkillScopeUser},
	})
	require.NoError(t, s.Load(context.Background()))

	got := s.List()
	require.Len(t, got, 2)

	byName := map[string]domain.Skill{}
	for _, sk := range got {
		byName[sk.Name] = sk
	}
	require.Equal(t, "Project version.", byName["shared"].Description)
	require.Equal(t, domain.SkillScopeProject, byName["shared"].Scope)
	require.Equal(t, domain.SkillScopeUser, byName["user-only"].Scope)
}

// TestPrecedence_AgentsMiddleScope locks in the three-way precedence introduced
// for the .agents/skills open standard: project > agents > user, first match
// wins on a name collision.
func TestPrecedence_AgentsMiddleScope(t *testing.T) {
	projDir := t.TempDir()
	agentsDir := t.TempDir()
	userDir := t.TempDir()

	writeSkill(t, projDir, "all-three", validSkillBody("all-three", "Project version."))
	writeSkill(t, agentsDir, "all-three", validSkillBody("all-three", "Agents version."))
	writeSkill(t, userDir, "all-three", validSkillBody("all-three", "User version."))

	writeSkill(t, agentsDir, "agents-and-user", validSkillBody("agents-and-user", "Agents version."))
	writeSkill(t, userDir, "agents-and-user", validSkillBody("agents-and-user", "User version."))

	writeSkill(t, agentsDir, "agents-only", validSkillBody("agents-only", "Only in .agents scope."))
	writeSkill(t, userDir, "user-only", validSkillBody("user-only", "Only in user scope."))

	s := newWithScopes(enabledCfg(), []scopedDir{
		{dir: projDir, scope: domain.SkillScopeProject},
		{dir: agentsDir, scope: domain.SkillScopeAgents},
		{dir: userDir, scope: domain.SkillScopeUser},
	})
	require.NoError(t, s.Load(context.Background()))

	got := s.List()
	require.Len(t, got, 4)

	byName := map[string]domain.Skill{}
	for _, sk := range got {
		byName[sk.Name] = sk
	}

	require.Equal(t, "Project version.", byName["all-three"].Description)
	require.Equal(t, domain.SkillScopeProject, byName["all-three"].Scope)

	require.Equal(t, "Agents version.", byName["agents-and-user"].Description)
	require.Equal(t, domain.SkillScopeAgents, byName["agents-and-user"].Scope)

	require.Equal(t, domain.SkillScopeAgents, byName["agents-only"].Scope)
	require.Equal(t, domain.SkillScopeUser, byName["user-only"].Scope)
}

// TestSearchScopes_Order guards the scan order that the precedence dedup relies
// on: project (.infer/skills), then the open-standard .agents/skills, then
// user-global (~/.infer/skills), then enabled plugins in registry order.
func TestSearchScopes_Order(t *testing.T) {
	pluginsDir := t.TempDir()
	cfg := pluginTestCfg(pluginsDir, config.PluginEntry{Name: "last-plugin", Enabled: true})
	scopes := New(cfg).searchScopes()

	require.GreaterOrEqual(t, len(scopes), 3)
	require.Equal(t, domain.SkillScopeProject, scopes[0].scope)
	require.Equal(t, filepath.Join(config.ConfigDirName, skillsSubdir), scopes[0].dir)

	require.Equal(t, domain.SkillScopeAgents, scopes[1].scope)
	require.Equal(t, filepath.Join(config.AgentsDirName, skillsSubdir), scopes[1].dir)

	last := scopes[len(scopes)-1]
	require.Equal(t, domain.SkillScopePlugin, last.scope)
	require.Equal(t, filepath.Join(pluginsDir, "last-plugin", skillsSubdir), last.dir)

	if home, err := os.UserHomeDir(); err == nil {
		require.Len(t, scopes, 4)
		require.Equal(t, domain.SkillScopeUser, scopes[2].scope)
		require.Equal(t, filepath.Join(home, config.ConfigDirName, skillsSubdir), scopes[2].dir)
	}
}

func TestDisabledFilter(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "alpha", validSkillBody("alpha", "First."))
	writeSkill(t, tmp, "beta", validSkillBody("beta", "Second."))
	writeSkill(t, tmp, "gamma", validSkillBody("gamma", "Third."))

	cfg := enabledCfg()
	cfg.Agent.Skills.DisabledSkills = []string{"beta"}

	s := newWithScopes(cfg, scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	got := s.List()
	require.Len(t, got, 2)

	names := []string{got[0].Name, got[1].Name}
	require.NotContains(t, names, "beta")
	require.Contains(t, names, "alpha")
	require.Contains(t, names, "gamma")
}

func TestPortability_AnthropicSkillFolder(t *testing.T) {
	tmp := t.TempDir()
	skillDir := filepath.Join(tmp, "skill-creator")
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "references"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(skillDir, "scripts"), 0o755))

	skillBody := `---
name: skill-creator
description: Create a new agent skill following the cross-vendor contract. Use when the user asks to author a skill.
license: Apache-2.0
---

# Skill Creator

See references/format.md for the spec.

## Scripts

` + "`scripts/init.sh`" + ` scaffolds a new skill directory.
`
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "references", "format.md"), []byte("# Format\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "scripts", "init.sh"), []byte("#!/bin/bash\n"), 0o755))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	got := s.List()
	require.Len(t, got, 1, "expected 1 skill loaded, errors: %+v", s.Errors())
	require.Equal(t, "skill-creator", got[0].Name)
	require.Empty(t, s.Errors())
}

func TestLoad_NonexistentScopesIgnored(t *testing.T) {
	s := newWithScopes(enabledCfg(), []scopedDir{
		{dir: "/nonexistent/path/that/should/not/exist", scope: domain.SkillScopeProject},
	})
	require.NoError(t, s.Load(context.Background()))
	require.Empty(t, s.List())
	require.Empty(t, s.Errors())
}

func TestLoad_SkipsDirsWithoutSkillMD(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "not-a-skill"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "not-a-skill", "README.md"), []byte("hi"), 0o644))

	writeSkill(t, tmp, "real-skill", validSkillBody("real-skill", "Real."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Len(t, s.List(), 1)
	require.Empty(t, s.Errors())
}

func TestParse_MalformedFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "broken", "---\nname: broken\ndescription: oops\n\n# Body without closing delim\n")

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
}

func TestParse_NoFrontmatter(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "plain", "# Just a markdown file with no frontmatter at all\n")

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	require.Empty(t, s.List())
	require.Len(t, s.Errors(), 1)
	require.Contains(t, s.Errors()[0].Reason, "frontmatter")
}

func TestGet(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "pdf-helper", validSkillBody("pdf-helper", "Extract text from PDFs."))

	s := newWithScopes(enabledCfg(), scope(tmp))
	require.NoError(t, s.Load(context.Background()))

	t.Run("hit", func(t *testing.T) {
		sk, ok := s.Get("pdf-helper")
		require.True(t, ok)
		require.Equal(t, "pdf-helper", sk.Name)
	})

	t.Run("miss", func(t *testing.T) {
		_, ok := s.Get("nope")
		require.False(t, ok)
	})
}

// pluginTestCfg returns an enabled config whose plugins registry points at
// dir and lists the given entries.
func pluginTestCfg(dir string, entries ...config.PluginEntry) *config.Config {
	cfg := enabledCfg()
	cfg.Plugins = *config.DefaultPluginsConfig()
	cfg.Plugins.Dir = dir
	cfg.Plugins.Plugins = entries
	return cfg
}

// writePluginSkill creates <pluginsDir>/<plugin>/skills/<name>/SKILL.md.
func writePluginSkill(t *testing.T, pluginsDir, plugin, name, description string) {
	t.Helper()
	writeSkill(t, filepath.Join(pluginsDir, plugin, "skills"), name, validSkillBody(name, description))
}

func TestSearchScopes_IncludesEnabledPluginSkills(t *testing.T) {
	t.Chdir(t.TempDir())
	pluginsDir := t.TempDir()
	writePluginSkill(t, pluginsDir, "ponytail", "ponytail", "Lazy senior dev mode.")
	writePluginSkill(t, pluginsDir, "off-plugin", "hidden", "Should not load.")

	cfg := pluginTestCfg(pluginsDir,
		config.PluginEntry{Name: "ponytail", Enabled: true},
		config.PluginEntry{Name: "off-plugin", Enabled: false},
	)
	s := New(cfg)
	require.NoError(t, s.Load(context.Background()))

	sk, ok := s.Get("ponytail")
	require.True(t, ok, "enabled plugin skill must be discovered")
	require.Equal(t, domain.SkillScopePlugin, sk.Scope)

	_, ok = s.Get("hidden")
	require.False(t, ok, "disabled plugin's skills must not be scanned")
}

func TestSearchScopes_PluginsMasterSwitchOff(t *testing.T) {
	t.Chdir(t.TempDir())
	pluginsDir := t.TempDir()
	writePluginSkill(t, pluginsDir, "ponytail", "ponytail", "Lazy senior dev mode.")

	cfg := pluginTestCfg(pluginsDir, config.PluginEntry{Name: "ponytail", Enabled: true})
	cfg.Plugins.Enabled = false
	s := New(cfg)
	require.NoError(t, s.Load(context.Background()))

	_, ok := s.Get("ponytail")
	require.False(t, ok)
}

func TestPrecedence_ProjectOverridesPlugin(t *testing.T) {
	projectRoot := t.TempDir()
	t.Chdir(projectRoot)
	writeSkill(t, filepath.Join(config.ConfigDirName, "skills"), "shared-name",
		validSkillBody("shared-name", "Project version wins."))

	pluginsDir := t.TempDir()
	writePluginSkill(t, pluginsDir, "some-plugin", "shared-name", "Plugin version loses.")

	cfg := pluginTestCfg(pluginsDir, config.PluginEntry{Name: "some-plugin", Enabled: true})
	s := New(cfg)
	require.NoError(t, s.Load(context.Background()))

	sk, ok := s.Get("shared-name")
	require.True(t, ok)
	require.Equal(t, domain.SkillScopeProject, sk.Scope)
	require.Contains(t, sk.Description, "Project version wins")
}

func TestLoadSkillMetadata_ExportedValidation(t *testing.T) {
	tmp := t.TempDir()
	writeSkill(t, tmp, "good-skill", validSkillBody("good-skill", "Valid."))
	sk, loadErr := LoadSkillMetadata(filepath.Join(tmp, "good-skill"), "good-skill", domain.SkillScopePlugin)
	require.Nil(t, loadErr)
	require.NotNil(t, sk)
	require.Equal(t, domain.SkillScopePlugin, sk.Scope)

	writeSkill(t, tmp, "bad-skill", "---\ndescription: missing name\n---\n")
	sk, loadErr = LoadSkillMetadata(filepath.Join(tmp, "bad-skill"), "bad-skill", domain.SkillScopePlugin)
	require.Nil(t, sk)
	require.NotNil(t, loadErr)
}
