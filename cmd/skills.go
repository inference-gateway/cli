package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	skills "github.com/inference-gateway/cli/internal/services/skills"
)

var skillsCmd = &cobra.Command{
	Use:   "skills",
	Short: "Manage Agent Skills",
	Long: `Manage Agent Skills.

Skills are folders containing a SKILL.md file with YAML frontmatter (name,
description). The format matches the contract shared by the official spec, so existing skill folders authored for
any of them can be dropped into .infer/skills/ (or the .agents/skills/ open standard) unchanged.

Locations scanned (highest precedence first; first match wins on name collision):
  - .infer/skills/<name>/SKILL.md       (project)
  - .agents/skills/<name>/SKILL.md      (open standard)
  - ~/.infer/skills/<name>/SKILL.md     (user-global)

Skills are enabled by default - disable via agent.skills.enabled=false in config or
INFER_AGENT_SKILLS_ENABLED=false. The list command always works regardless of
the enable flag so you can verify discovery.`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered skills",
	Long: `List discovered Agent Skills from .infer/skills/, .agents/skills/, and ~/.infer/skills/.

Output includes each skill's name, scope (project / agents / user), one-line
description, and the absolute path to its SKILL.md. Validation errors for
skipped skills are shown so you can fix authoring mistakes.`,
	RunE: listSkills,
}

func listSkills(cmd *cobra.Command, _ []string) error {
	scanCfg := &config.Config{
		Agent: config.AgentConfig{
			Skills: config.AgentSkillsConfig{Enabled: true},
		},
	}

	svc := skills.New(scanCfg)
	if err := svc.Load(context.Background()); err != nil {
		return fmt.Errorf("failed to load skills: %w", err)
	}

	loaded := svc.List()
	errs := svc.Errors()

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		out := map[string]any{
			"skills": loaded,
			"errors": errs,
			"total":  len(loaded),
		}
		data, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal skills: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(loaded) == 0 && len(errs) == 0 {
		fmt.Println("No skills discovered.")
		fmt.Println("Drop a skill folder into .infer/skills/<name>/ with a SKILL.md file.")
		fmt.Println("See `infer skills --help` for the format.")
		return nil
	}

	enabledInConfig := Cfg != nil && Cfg.Agent.Skills.Enabled
	if !enabledInConfig {
		fmt.Println("Note: skills are currently disabled in config (agent.skills.enabled=false). Listing them anyway for visibility.")
		fmt.Println()
	}

	fmt.Println(listTitle(fmt.Sprintf("Discovered Skills (%d)", len(loaded))))
	fmt.Println()

	skillsTable := newListTable("Name", "Scope", "Description", "Path")
	for _, sk := range loaded {
		skillsTable.Row(sk.Name, string(sk.Scope), sk.Description, sk.Path)
	}
	fmt.Println(skillsTable.Render())

	if len(errs) > 0 {
		fmt.Println()
		fmt.Println(listTitle(fmt.Sprintf("Skipped (%d)", len(errs))))
		fmt.Println()
		errTable := newListTable("Path", "Reason")
		for _, e := range errs {
			errTable.Row(e.Path, e.Reason)
		}
		fmt.Println(errTable.Render())
	}

	return nil
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install <skill | org/skill | github-url>",
	Short: "Install a skill from a GitHub repository",
	Long: `Install a skill folder from a GitHub repository.

You can pass any of the following:

  - A skill name:           skill-creator
      → https://github.com/inference-gateway/skills/tree/main/skills/skill-creator
  - An <org>/<skill> pair:  acme/skill-creator
      → https://github.com/acme/skills/tree/main/skills/skill-creator
  - A full GitHub tree URL: https://github.com/<owner>/<repo>/tree/<ref>/<path>

Shorthand forms assume the skill lives under skills/<name>/ inside a repo
named "skills" on the given org, and resolve against the "main" branch.
For any other layout, branch, or tag, use the full URL form.

Examples:
  infer skills install skill-creator
  infer skills install acme/internal-comms
  infer skills install https://github.com/anthropics/skills/tree/main/skills/pdf

By default the skill is written to .infer/skills/<dirname>/. Pass --user
to install to ~/.infer/skills/ instead. Pass --overwrite to replace an
existing skill folder of the same name.

After download, the same frontmatter validator that runs at startup runs
against the downloaded folder. If validation fails the folder is removed
and the reason is printed.

Requests are unauthenticated by default, which GitHub limits to 60 API
requests per hour per IP. Set GITHUB_TOKEN (or GH_TOKEN) in the environment
to authenticate: this raises the limit to 5,000 requests per hour and allows
installing from private repositories the token can access.`,
	Args: cobra.ExactArgs(1),
	RunE: installSkill,
}

func installSkill(cmd *cobra.Command, args []string) error {
	rawURL := args[0]
	userScope, _ := cmd.Flags().GetBool("user")
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	destBase, err := resolveSkillsDest(userScope)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(destBase, 0755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	dest, err := skills.NewInstaller().InstallFromGitHub(cmd.Context(), rawURL, destBase, overwrite)
	if err != nil {
		return err
	}
	fmt.Printf("Installed skill to %s\n", dest)
	if Cfg == nil || !Cfg.Agent.Skills.Enabled {
		fmt.Println("Note: skills are disabled - run `infer config set agent.skills.enabled true` (or set INFER_AGENT_SKILLS_ENABLED=true) to load this skill.")
	}
	return nil
}

func resolveSkillsDest(userScope bool) (string, error) {
	if userScope {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory: %w", err)
		}
		return filepath.Join(home, config.ConfigDirName, "skills"), nil
	}
	if _, err := os.Stat(config.ConfigDirName); os.IsNotExist(err) {
		return "", fmt.Errorf("%s/ not found in current directory; run `infer init` first or pass --user", config.ConfigDirName)
	}
	return filepath.Join(config.ConfigDirName, "skills"), nil
}

var skillsUninstallCmd = &cobra.Command{
	Use:   "uninstall <name>",
	Short: "Uninstall a skill by name",
	Long: `Uninstall a skill by removing its folder from the local skills directory.

The name must be the on-disk skill directory name (matching the skill's
frontmatter name), e.g. "pdf" for a skill at .infer/skills/pdf/. Pass --user
to look in ~/.infer/skills/ instead. There is no confirmation prompt.

Example:
  infer skills uninstall pdf
  infer skills uninstall --user internal-comms`,
	Args: cobra.ExactArgs(1),
	RunE: uninstallSkill,
}

func uninstallSkill(cmd *cobra.Command, args []string) error {
	name := args[0]
	userScope, _ := cmd.Flags().GetBool("user")

	destBase, err := resolveSkillsDest(userScope)
	if err != nil {
		return err
	}

	removed, err := skills.Uninstall(name, destBase)
	if err != nil {
		return err
	}
	fmt.Printf("Uninstalled skill %s (%s)\n", name, removed)
	return nil
}

func init() {
	skillsCmd.AddCommand(skillsListCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsUninstallCmd)
	skillsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	skillsInstallCmd.Flags().Bool("user", false, "Install to ~/.infer/skills instead of project-local")
	skillsInstallCmd.Flags().Bool("overwrite", false, "Replace an existing skill folder of the same name")
	skillsUninstallCmd.Flags().Bool("user", false, "Look up the skill under ~/.infer/skills instead of project-local")
	rootCmd.AddCommand(skillsCmd)
}
