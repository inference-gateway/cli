package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	skills "github.com/inference-gateway/cli/internal/skills"
)

// searchDescWidth keeps the search table inside a normal terminal. Catalog
// descriptions run to several hundred columns, which wraps the borders into
// unreadable soup; --no-trunc prints them in full.
const searchDescWidth = 80

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
	Long: `List discovered Agent Skills from .infer/skills/, .agents/skills/,
~/.infer/skills/, and enabled plugins (~/.infer/plugins/<name>/skills/).

Output includes each skill's name, scope (project / agents / user / plugin),
one-line description, and the absolute path to its SKILL.md. Validation errors
for skipped skills are shown so you can fix authoring mistakes.`,
	RunE: listSkills,
}

func listSkills(cmd *cobra.Command, _ []string) error {
	scanCfg := &config.Config{
		Agent: config.AgentConfig{
			Skills: config.AgentSkillsConfig{Enabled: true},
		},
	}
	if Cfg != nil {
		scanCfg.Plugins = Cfg.Plugins
		scanCfg.Agent.Skills.Discovery = Cfg.Agent.Skills.Discovery
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

var skillsSearchCmd = &cobra.Command{
	Use:   "search [term]",
	Short: "Search the skills catalog",
	Long: `Search the centralized skills catalog by name and description.

The catalog is published as a single catalog.json in the configured skills
repository (agent.skills.repository, default inference-gateway/skills), so
matching runs locally over the fetched index. Skill names are fuzzy-matched
and ranked by score; descriptions are matched on a literal substring.

Descriptions are truncated to keep the table readable - pass --no-trunc for
the full text.

Search works regardless of agent.skills.discovery.enabled: fetching the index
on an explicit search is a direct user action, not background discovery.
Omit the term to browse the head of the catalog.

Install anything it turns up with ` + "`infer skills install <name>`" + `.

Examples:
  infer skills search rust
  infer skills search "pull request" --limit 20
  infer skills search --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: searchSkills,
}

func searchSkills(cmd *cobra.Command, args []string) error {
	var query string
	if len(args) == 1 {
		query = args[0]
	}
	limit, _ := cmd.Flags().GetInt("limit")

	searchCfg := Cfg
	if searchCfg == nil {
		searchCfg = config.DefaultConfig()
	}

	catalog := skills.NewCatalogClient(searchCfg)
	matches := catalog.Search(cmd.Context(), query, limit)
	release, updated := catalog.Release()
	installed := installedSkillPaths()

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		out := make([]map[string]any, 0, len(matches))
		for _, sk := range matches {
			out = append(out, map[string]any{
				"name":        sk.Name,
				"description": sk.Description,
				"installed":   installed[sk.Name] != "",
				"path":        installed[sk.Name],
			})
		}
		data, err := json.MarshalIndent(map[string]any{
			"skills":  out,
			"total":   len(out),
			"release": release,
			"updated": updated,
		}, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal search results: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(matches) == 0 {
		fmt.Printf("No catalog skills matched %q.\n", query)
		fmt.Println("The catalog is fetched from the repository in agent.skills.repository; check `infer skills search` with no term to confirm it is reachable.")
		return nil
	}

	fmt.Println(listTitle(fmt.Sprintf("Catalog Skills (%d)", len(matches))))
	if line := catalogVersionLine(release, updated); line != "" {
		fmt.Println(listField("Catalog", line))
	}
	fmt.Println()

	noTrunc, _ := cmd.Flags().GetBool("no-trunc")
	table := newListTable("Name", "Description", "Installed")
	for _, sk := range matches {
		state := shortenPath(installed[sk.Name])
		if state == "" {
			state = "no"
		}
		desc := sk.Description
		if !noTrunc {
			desc = formatting.TruncateText(desc, searchDescWidth)
		}
		table.Row(sk.Name, desc, state)
	}
	fmt.Println(table.Render())
	fmt.Println()
	fmt.Println("Install one with `infer skills install <name>`.")

	return nil
}

// catalogVersionLine renders the catalog's release and update date, e.g.
// "0.9.1 (updated 2026-07-30)". The catalog is versioned as a whole - entries
// carry no version of their own - so this describes the whole result set rather
// than a per-row column. Empty when the catalog publishes neither.
func catalogVersionLine(release, updated string) string {
	if ts, err := time.Parse(time.RFC3339, updated); err == nil {
		updated = ts.Local().Format("2006-01-02")
	}
	switch {
	case release != "" && updated != "":
		return fmt.Sprintf("%s (updated %s)", release, updated)
	case release != "":
		return release
	case updated != "":
		return fmt.Sprintf("updated %s", updated)
	default:
		return ""
	}
}

// installedSkillPaths maps each locally present skill name to its SKILL.md, so
// search results can point at the file rather than just saying "yes". Discovery
// stays off here - the catalog side is what Search already fetched.
func installedSkillPaths() map[string]string {
	scanCfg := &config.Config{
		Agent: config.AgentConfig{
			Skills: config.AgentSkillsConfig{Enabled: true},
		},
	}
	if Cfg != nil {
		scanCfg.Plugins = Cfg.Plugins
	}

	svc := skills.New(scanCfg)
	if err := svc.Load(context.Background()); err != nil {
		return nil
	}

	paths := make(map[string]string)
	for _, sk := range svc.List() {
		paths[sk.Name] = sk.Path
	}
	return paths
}

// shortenPath renders an absolute skill path for the search table: relative to
// the working directory when it lives under it, ~-prefixed when under $HOME,
// absolute otherwise. A full path is 80+ columns and wraps the table borders.
func shortenPath(path string) string {
	if path == "" {
		return ""
	}
	if cwd, err := os.Getwd(); err == nil {
		if rel, relErr := filepath.Rel(cwd, path); relErr == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, relErr := filepath.Rel(home, path); relErr == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return path
}

var skillsInstallCmd = &cobra.Command{
	Use:   "install <skill | org/skill | github-url>",
	Short: "Install a skill from a GitHub repository",
	Long: `Install a skill folder from a GitHub repository.

You can pass any of the following:

  - A skill name:           skill-creator
      → the source recorded in the catalog, else
        https://github.com/inference-gateway/skills/tree/main/skills/skill-creator
  - An <org>/<skill> pair:  acme/skill-creator
      → https://github.com/acme/skills/tree/main/skills/skill-creator
  - A full GitHub tree URL: https://github.com/<owner>/<repo>/tree/<ref>/<path>

A bare name resolves to its catalog source first, so a skill sourced from another
repo (e.g. under .agents/skills/<name>/) installs by name. When the catalog is
unavailable or does not list it, the bare name and the <org>/<skill> form assume
the skill lives under skills/<name>/ in a repo named "skills" on the given org,
on the "main" branch. For any other layout, branch, or tag, use the full URL form.

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

	repository := config.DefaultSkillsRepository
	if Cfg != nil {
		repository = Cfg.Agent.Skills.SkillsRepository()
		if src, ok := skills.NewCatalogClient(Cfg).ResolveInstallURL(cmd.Context(), rawURL); ok {
			rawURL = src
		}
	}

	dest, err := skills.NewInstaller(repository).
		InstallFromGitHub(cmd.Context(), rawURL, destBase, overwrite)
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
	skillsCmd.AddCommand(skillsSearchCmd)
	skillsCmd.AddCommand(skillsInstallCmd)
	skillsCmd.AddCommand(skillsUninstallCmd)
	skillsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	skillsSearchCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	skillsSearchCmd.Flags().Int("limit", 10, "Maximum number of matches to return")
	skillsSearchCmd.Flags().Bool("no-trunc", false, "Print full descriptions instead of truncating them to fit the table")
	skillsInstallCmd.Flags().Bool("user", false, "Install to ~/.infer/skills instead of project-local")
	skillsInstallCmd.Flags().Bool("overwrite", false, "Replace an existing skill folder of the same name")
	skillsUninstallCmd.Flags().Bool("user", false, "Look up the skill under ~/.infer/skills instead of project-local")
	rootCmd.AddCommand(skillsCmd)
}
