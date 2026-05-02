package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
any of them can be dropped into .infer/skills/ unchanged.

Locations scanned (project precedes user-global on name collision):
  - .infer/skills/<name>/SKILL.md       (project)
  - ~/.infer/skills/<name>/SKILL.md     (user-global)

Skills are disabled by default — enable via agent.skills.enabled in config or
INFER_AGENT_SKILLS_ENABLED=true. The list command always works regardless of
the enable flag so you can verify discovery.`,
}

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List discovered skills",
	Long: `List discovered Agent Skills from .infer/skills/ and ~/.infer/skills/.

Output includes each skill's name, scope (project / user), one-line description,
and the absolute path to its SKILL.md. Validation errors for skipped skills are
shown so you can fix authoring mistakes.`,
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

	var md strings.Builder
	fmt.Fprintf(&md, "**DISCOVERED SKILLS:** %d\n\n", len(loaded))
	md.WriteString("| Name | Scope | Description | Path |\n")
	md.WriteString("|------|-------|-------------|------|\n")
	for _, sk := range loaded {
		fmt.Fprintf(&md, "| %s | %s | %s | %s |\n", sk.Name, sk.Scope, sk.Description, sk.Path)
	}

	if len(errs) > 0 {
		fmt.Fprintf(&md, "\n**SKIPPED (%d):**\n\n", len(errs))
		md.WriteString("| Path | Reason |\n")
		md.WriteString("|------|--------|\n")
		for _, e := range errs {
			fmt.Fprintf(&md, "| %s | %s |\n", e.Path, e.Reason)
		}
	}

	rendered, err := renderMarkdown(md.String())
	if err != nil {
		fmt.Print(md.String())
		return nil
	}
	fmt.Print(rendered)
	return nil
}

func init() {
	skillsCmd.AddCommand(skillsListCmd)
	skillsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.AddCommand(skillsCmd)
}
