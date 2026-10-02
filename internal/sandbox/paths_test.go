package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
)

func TestValidatePathInSandbox_SkillsCarveOut(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir available")
	}

	userSkill := filepath.Join(home, config.ConfigDirName, "skills", "demo", "SKILL.md")
	projectSkill, err := filepath.Abs(filepath.Join(config.ConfigDirName, "skills", "demo", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to resolve project skill path: %v", err)
	}

	relSkill := filepath.Join(config.ConfigDirName, "skills", "demo", "SKILL.md")

	t.Run("skills enabled (default): carve-out grants read access", func(t *testing.T) {
		cfg := config.DefaultConfig()
		if !cfg.Agent.Skills.Enabled {
			t.Fatalf("expected skills enabled by default")
		}

		t.Run("user skills dir allowed", func(t *testing.T) {
			if err := ValidateRead(cfg, userSkill); err != nil {
				t.Fatalf("expected %s allowed, got %v", userSkill, err)
			}
		})

		t.Run("project skills dir allowed", func(t *testing.T) {
			if err := ValidateRead(cfg, projectSkill); err != nil {
				t.Fatalf("expected %s allowed, got %v", projectSkill, err)
			}
		})

		t.Run("relative project skill path allowed", func(t *testing.T) {
			if err := ValidateRead(cfg, relSkill); err != nil {
				t.Fatalf("expected relative %s allowed, got %v", relSkill, err)
			}
		})

		t.Run("user config.yaml still denied (protected paths)", func(t *testing.T) {
			denied := filepath.Join(home, config.ConfigDirName, "config.yaml")
			if err := ValidateRead(cfg, denied); err == nil {
				t.Fatalf("expected %s to be denied", denied)
			}
		})

		t.Run("user conversations.db still denied (protected paths)", func(t *testing.T) {
			denied := filepath.Join(home, config.ConfigDirName, "conversations.db")
			if err := ValidateRead(cfg, denied); err == nil {
				t.Fatalf("expected %s to be denied", denied)
			}
		})

		t.Run("lookalike sibling dir not allowed", func(t *testing.T) {
			sibling := filepath.Join(home, config.ConfigDirName, "skills-evil", "SKILL.md")
			if err := ValidateRead(cfg, sibling); err == nil {
				t.Fatalf("expected sibling %s rejected (prefix must be a path boundary)", sibling)
			}
		})

		t.Run("protected paths under skills dir still block", func(t *testing.T) {
			secret := filepath.Join(home, config.ConfigDirName, "skills", "demo", "creds.env")
			if err := ValidateRead(cfg, secret); err == nil {
				t.Fatalf("expected protected file %s to be denied", secret)
			}
		})
	})

	t.Run("skills disabled: carve-out is off, skills dir denied", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Agent.Skills.Enabled = false

		for _, p := range []string{userSkill, projectSkill, relSkill} {
			if err := ValidateRead(cfg, p); err == nil {
				t.Fatalf("expected %s denied while skills are disabled", p)
			}
		}
	})
}

// TestValidatePathInSandbox_AgentsSkillsCarveOut covers the .agents/skills open
// standard. Unlike .infer/, the .agents/ directory is not in ProtectedPaths, so
// the carve-out is only observable against a *restrictive* sandbox: with skills
// enabled, .agents/skills/** must be readable even though it sits outside the
// configured sandbox dirs (parity with .infer/skills), while non-skills paths
// under .agents and lookalike siblings stay denied.
func TestValidatePathInSandbox_AgentsSkillsCarveOut(t *testing.T) {
	sandboxDir := t.TempDir()

	agentsSkill, err := filepath.Abs(filepath.Join(config.AgentsDirName, "skills", "demo", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to resolve .agents skill path: %v", err)
	}
	relAgentsSkill := filepath.Join(config.AgentsDirName, "skills", "demo", "SKILL.md")
	agentsRef := filepath.Join(config.AgentsDirName, "skills", "demo", "references", "guide.md")
	agentsNonSkill := filepath.Join(config.AgentsDirName, "config.yaml")
	agentsLookalike := filepath.Join(config.AgentsDirName, "skills-evil", "demo", "SKILL.md")

	t.Run("skills enabled: .agents/skills carved out of a restrictive sandbox", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Tools.Sandbox.Directories = []string{sandboxDir}
		if !cfg.Agent.Skills.Enabled {
			t.Fatalf("expected skills enabled by default")
		}

		for _, p := range []string{agentsSkill, relAgentsSkill, agentsRef} {
			if err := ValidateRead(cfg, p); err != nil {
				t.Fatalf("expected %s allowed via carve-out, got %v", p, err)
			}
		}

		for _, p := range []string{agentsNonSkill, agentsLookalike} {
			if err := ValidateRead(cfg, p); err == nil {
				t.Fatalf("expected %s denied (not a skills path, outside sandbox)", p)
			}
		}
	})

	t.Run("skills disabled: .agents/skills denied by the restrictive sandbox", func(t *testing.T) {
		cfg := config.DefaultConfig()
		cfg.Tools.Sandbox.Directories = []string{sandboxDir}
		cfg.Agent.Skills.Enabled = false

		for _, p := range []string{agentsSkill, relAgentsSkill} {
			if err := ValidateRead(cfg, p); err == nil {
				t.Fatalf("expected %s denied while skills are disabled", p)
			}
		}
	})
}

// TestValidatePathInSandbox_ConfigDir locks in the directory-wide protection of
// the config dir: sensitive config files are denied wholesale, and the old
// project-local .infer/tmp is no longer a sandbox carve-out (runtime
// artifacts moved to ~/.infer/projects/<project-slug>/). This config has no
// configDir set, so GetConfigDir() is the relative ".infer" - the case where a
// config-relative check would miss the userspace runtime dirs entirely, so the
// ~/.infer plans and artifacts entries below are the ones that matter here.
// Hard protections like *.env apply everywhere.
func TestValidatePathInSandbox_ConfigDir(t *testing.T) {
	cfg := config.DefaultConfig()

	denied := []string{
		config.ConfigDirName + "/config.yaml",
		config.ConfigDirName + "/agents.yaml",
		config.ConfigDirName + "/conversations.db",
		config.ConfigDirName + "/shortcuts/git.yaml",
		config.ConfigDirName + "/tmp/scratch.txt",
		config.ConfigDirName + "/tmp/leaked.env",
	}
	for _, p := range denied {
		t.Run("deny "+p, func(t *testing.T) {
			if err := ValidateRead(cfg, p); err == nil {
				t.Fatalf("expected %s to be denied", p)
			}
		})
	}

	allowed := []string{
		config.ConfigDirName + "/plans/2026-06-01-do-thing.md",
		filepath.Join(config.ProjectRuntimeDir(), "tmp", "scratch.txt"),
		filepath.Join(config.ProjectRuntimeDir(), "backups", "main.go.backup"),
		filepath.Join(config.UserSpaceConfigDir(), config.ArtifactsDirName, "run-1", "report.md"),
		filepath.Join(config.UserSpaceConfigDir(), "plans", "2026-06-01-do-thing.md"),
		filepath.Join(config.UserSpaceConfigDir(), "tmp", "uploads", "18d5495ba8b83fb8.jpg"),
	}
	for _, p := range allowed {
		t.Run("allow "+p, func(t *testing.T) {
			if err := ValidateRead(cfg, p); err != nil {
				t.Fatalf("expected %s allowed, got %v", p, err)
			}
		})
	}
}

// TestValidatePathInSandbox_GoLibCarveOut locks in that the Go module cache is
// readable via the carve-out but rejected for writes — including via relative
// paths, which must be resolved before the read-only check (regression: raw
// relative paths used to bypass ValidateWrite's prefix match).
func TestValidatePathInSandbox_GoLibCarveOut(t *testing.T) {
	modcache := t.TempDir()
	t.Setenv("GOMODCACHE", modcache)
	cfg := config.DefaultConfig()

	target := filepath.Join(modcache, "github.com", "some", "mod@v1.0.0", "file.go")

	t.Run("read inside modcache allowed", func(t *testing.T) {
		if err := ValidateRead(cfg, target); err != nil {
			t.Fatalf("expected %s readable, got %v", target, err)
		}
	})

	t.Run("write inside modcache rejected (absolute)", func(t *testing.T) {
		if err := ValidateWrite(cfg, target); err == nil {
			t.Fatalf("expected write to %s rejected", target)
		}
	})

	t.Run("write inside modcache rejected (relative)", func(t *testing.T) {
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd: %v", err)
		}
		rel, err := filepath.Rel(cwd, target)
		if err != nil {
			t.Skipf("no relative path from %s to %s: %v", cwd, target, err)
		}
		if err := ValidateRead(cfg, rel); err != nil {
			t.Fatalf("expected relative %s readable, got %v", rel, err)
		}
		if err := ValidateWrite(cfg, rel); err == nil {
			t.Fatalf("expected write to relative %s rejected", rel)
		}
	})

	t.Run("write in sandbox still allowed", func(t *testing.T) {
		if err := ValidateWrite(cfg, "somefile.txt"); err != nil {
			t.Fatalf("expected sandbox write allowed, got %v", err)
		}
	})
}

// TestValidatePathInSandbox_ConfigDirUserspace locks in that the tmp/plans
// carve-out also covers the resolved userspace config dir (~/.infer). config.When the
// config is loaded from the userspace location, GetConfigDir() returns an
// absolute home path and plans are written there - the sandbox must still
// allow the agent to read them back.
func TestValidatePathInSandbox_ConfigDirUserspace(t *testing.T) {
	cfg := config.DefaultConfig()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}

	userspaceConfigDir := filepath.Join(homeDir, config.ConfigDirName)
	cfg.SetConfigDir(userspaceConfigDir)

	allowed := []string{
		filepath.Join(userspaceConfigDir, "plans", "2026-06-01-do-thing.md"),
		filepath.Join(userspaceConfigDir, "projects.yaml"),
		filepath.Join(userspaceConfigDir, "tmp", "uploads", "18d5495ba8b83fb8.jpg"),
		filepath.Join(userspaceConfigDir, "tmp", "scratch.txt"),
		filepath.Join(config.ProjectRuntimeDir(), "artifacts", "sess-1", "image.png"),
		filepath.Join(config.ProjectRuntimeDir(), "exports", "chat_export_1.md"),
		filepath.Join(userspaceConfigDir, config.ArtifactsDirName, "run-1", "report.md"),
	}
	for _, p := range allowed {
		t.Run("allow "+p, func(t *testing.T) {
			if err := ValidateRead(cfg, p); err != nil {
				t.Fatalf("expected %s allowed, got %v", p, err)
			}
		})
	}

	denied := []string{
		filepath.Join(userspaceConfigDir, "config.yaml"),
		filepath.Join(userspaceConfigDir, "agents.yaml"),
	}
	for _, p := range denied {
		t.Run("deny "+p, func(t *testing.T) {
			if err := ValidateRead(cfg, p); err == nil {
				t.Fatalf("expected %s to be denied", p)
			}
		})
	}

	t.Run("projects.yaml is writable", func(t *testing.T) {
		p := filepath.Join(userspaceConfigDir, "projects.yaml")
		if err := ValidateWrite(cfg, p); err != nil {
			t.Fatalf("expected %s writable, got %v", p, err)
		}
	})
}

func TestValidatePathInSandbox_PluginsCarveOut(t *testing.T) {
	t.Chdir(t.TempDir())
	pluginsDir := filepath.Join(config.ConfigDirName, config.PluginsDirName)
	cfg := config.DefaultConfig()
	cfg.Plugins = *config.DefaultPluginsConfig()
	cfg.Plugins.Dir = pluginsDir

	skillPath := filepath.Join(pluginsDir, "ponytail", "skills", "ponytail", "SKILL.md")
	require.NoError(t, ValidateRead(cfg, skillPath))

	envPath := filepath.Join(pluginsDir, "ponytail", ".env")
	require.Error(t, ValidateRead(cfg, envPath), "file-level protections must still apply inside the plugins dir")

	cfg.Plugins.Enabled = false
	require.Error(t, ValidateRead(cfg, skillPath), "carve-out must be gated on plugins.enabled")
}

// TestValidateWrite_SandboxPolicyFile locks in that the agent can never edit
// its own policy: both sandbox.yaml locations stay unwritable even when the
// user empties protected_paths, while reading them is still allowed.
func TestValidateWrite_SandboxPolicyFile(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	t.Chdir(project)
	t.Setenv("HOME", home)

	cfg := config.DefaultConfig()
	cfg.Tools.Sandbox.Directories = []string{project, home}
	cfg.Tools.Sandbox.ProtectedPaths = nil

	for _, file := range config.SandboxFilePaths() {
		require.NoError(t, ValidateRead(cfg, file), "reading %s", file)
		require.ErrorContains(t, ValidateWrite(cfg, file), "sandbox policy", "writing %s", file)
	}
	require.NoError(t, ValidateWrite(cfg, filepath.Join(project, config.ConfigDirName, "other.yaml")), "only the policy file is pinned")
}
