package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	plugins "github.com/inference-gateway/cli/internal/services/plugins"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

var pluginsCmd = &cobra.Command{
	Use:   "plugins",
	Short: "Manage Claude Code-format plugins",
	Long: `Install and manage Claude Code-format plugins from GitHub or local paths.

A plugin is a content package: SKILL.md skills (surfaced through the native
skills system with the "plugin" scope) and an optional AGENTS.md instruction
ruleset (injected into the system prompt while the plugin is enabled).
Plugin code (hooks/, commands/) is detected but NEVER executed.

Plugins are userspace-only: content lives under ~/.infer/plugins/<name>/ and
the registry is ~/.infer/plugins.yaml (created on first install).`,
}

var pluginsInstallCmd = &cobra.Command{
	Use:   "install <owner/repo | github-url | local-path>",
	Short: "Install a plugin from GitHub or a local directory",
	Long: `Install a plugin. Accepted sources:

  infer plugins install DietrichGebert/ponytail
  infer plugins install DietrichGebert/ponytail@v1.2.3
  infer plugins install https://github.com/DietrichGebert/ponytail
  infer plugins install ./path/to/plugin

Only the mapped content subset is downloaded (.claude-plugin/plugin.json,
AGENTS.md, skills/). The plugin's AGENTS.md becomes always-on system prompt
content, so the install summary asks for confirmation (skip with --yes).`,
	Args: cobra.ExactArgs(1),
	RunE: installPlugin,
}

var pluginsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed plugins",
	RunE:  listPlugins,
}

var pluginsRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an installed plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  removePlugin,
}

var pluginsUpdateCmd = &cobra.Command{
	Use:   "update [<name>]",
	Short: "Re-fetch one or all plugins from their install source",
	Args:  cobra.MaximumNArgs(1),
	RunE:  updatePlugins,
}

var pluginsEnableCmd = &cobra.Command{
	Use:   "enable <name>",
	Short: "Enable an installed plugin",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setPluginEnabled(args[0], true) },
}

var pluginsDisableCmd = &cobra.Command{
	Use:   "disable <name>",
	Short: "Disable an installed plugin (skills unloaded, instructions removed)",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return setPluginEnabled(args[0], false) },
}

func init() {
	pluginsCmd.AddCommand(pluginsInstallCmd)
	pluginsCmd.AddCommand(pluginsListCmd)
	pluginsCmd.AddCommand(pluginsRemoveCmd)
	pluginsCmd.AddCommand(pluginsUpdateCmd)
	pluginsCmd.AddCommand(pluginsEnableCmd)
	pluginsCmd.AddCommand(pluginsDisableCmd)

	pluginsInstallCmd.Flags().String("ref", "", "Git ref (branch, tag, or commit) to install from")
	pluginsInstallCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	pluginsInstallCmd.Flags().Bool("overwrite", false, "Replace an already-installed plugin with the same name")
	pluginsListCmd.Flags().StringP("format", "f", "text", "Output format (text|json)")

	rootCmd.AddCommand(pluginsCmd)
}

// pluginsRoot resolves the plugins storage root from the effective config.
func pluginsRoot() (string, error) {
	if Cfg != nil {
		return Cfg.Plugins.ResolveDir()
	}
	return config.PluginsConfig{}.ResolveDir()
}

// loadPluginsRegistry loads ~/.infer/plugins.yaml; the file is created on first save.
func loadPluginsRegistry() (*config.PluginsConfig, error) {
	registry, err := config.LoadPlugins(getPluginsConfigPath())
	if err != nil {
		return nil, fmt.Errorf("failed to load plugins registry: %w", err)
	}
	return registry, nil
}

func installPlugin(cmd *cobra.Command, args []string) error {
	src, err := plugins.ParseSource(args[0])
	if err != nil {
		return err
	}
	if ref, _ := cmd.Flags().GetString("ref"); ref != "" {
		src.Ref = ref
	}
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	yes, _ := cmd.Flags().GetBool("yes")

	root, err := pluginsRoot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("failed to create plugins dir: %w", err)
	}
	cleanupStaleStaging(root)

	staging := filepath.Join(root, fmt.Sprintf(".staging-%d", os.Getpid()))
	defer func() { _ = os.RemoveAll(staging) }()

	unsupported, err := plugins.NewInstaller().Stage(context.Background(), src, staging)
	if err != nil {
		return err
	}

	fallbackName := src.Repo
	if src.Kind == plugins.SourceLocal {
		fallbackName = filepath.Base(src.Path)
	}
	res, err := plugins.Inspect(staging, fallbackName)
	if err != nil {
		return err
	}
	res.Unsupported = unsupported

	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	existing, _ := registry.ReadEntry(res.Name)
	finalDir := filepath.Join(root, res.Name)
	if existing != nil && !overwrite {
		return fmt.Errorf("plugin %q is already installed (use --overwrite to replace)", res.Name)
	}

	printInstallSummary(res, src)
	if err := confirmInstall(yes); err != nil {
		return err
	}

	if err := plugins.Commit(staging, finalDir, overwrite); err != nil {
		return err
	}

	entry := config.PluginEntry{Name: res.Name, Source: src.Raw, Ref: src.Ref, Version: res.Version, Enabled: true}
	if existing != nil {
		entry.Enabled = existing.Enabled
		err = registry.UpdateEntry(entry)
	} else {
		err = registry.CreateEntry(entry)
	}
	if err != nil {
		return fmt.Errorf("plugin files installed to %s but registering failed: %w (re-run with --overwrite to retry)", finalDir, err)
	}

	fmt.Printf("%s Installed plugin %s to %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), res.Name, finalDir)
	printPostInstallNotes()
	return nil
}

// printInstallSummary tells the user what the plugin will activate before they confirm.
func printInstallSummary(res *plugins.InstallResult, src plugins.Source) {
	fmt.Println(listTitle(fmt.Sprintf("Plugin: %s", res.Name)))
	if res.Version != "" {
		fmt.Println(listField("Version", res.Version))
	}
	if res.Description != "" {
		fmt.Println(listField("Description", res.Description))
	}
	fmt.Println(listField("Source", src.Raw))

	if len(res.Skills) > 0 {
		names := make([]string, 0, len(res.Skills))
		for _, sk := range res.Skills {
			names = append(names, sk.Name)
		}
		fmt.Println(listField(fmt.Sprintf("Skills (%d)", len(res.Skills)), strings.Join(names, ", ")))
	}
	for _, skErr := range res.SkillErrors {
		fmt.Println(listHint(fmt.Sprintf("skipped invalid skill %s: %s", skErr.Path, skErr.Reason)))
	}
	if res.HasInstructions {
		fmt.Println(listField("Instructions", fmt.Sprintf("AGENTS.md (%d chars) - injected into EVERY system prompt while enabled", res.InstructionsLen)))
	}
	if len(res.Unsupported) > 0 {
		parts := make([]string, 0, len(res.Unsupported))
		for label, n := range res.Unsupported {
			parts = append(parts, fmt.Sprintf("%s/ (%d files)", label, n))
		}
		fmt.Println(listHint(fmt.Sprintf("detected but ignored: %s - infer does not execute plugin code", strings.Join(parts, ", "))))
	}
}

// confirmInstall prompts y/N on a TTY; non-interactive stdin errors unless --yes.
func confirmInstall(yes bool) error {
	if yes {
		return nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) == 0 {
		return fmt.Errorf("confirmation required on non-interactive stdin - pass --yes to proceed")
	}
	fmt.Print("Proceed? [y/N]: ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	default:
		return fmt.Errorf("installation aborted")
	}
}

func printPostInstallNotes() {
	if Cfg == nil {
		return
	}
	if !Cfg.Plugins.Enabled {
		fmt.Println(listHint("note: plugins are globally disabled (plugins.enabled: false / INFER_PLUGINS_ENABLED=false)"))
	}
	if !Cfg.Agent.Skills.Enabled {
		fmt.Println(listHint("note: agent.skills.enabled is false - plugin skills will not load (instructions still inject)"))
	}
}

// cleanupStaleStaging best-effort removes .staging-* leftovers from crashed installs.
func cleanupStaleStaging(root string) {
	matches, err := filepath.Glob(filepath.Join(root, ".staging-*"))
	if err != nil {
		return
	}
	for _, m := range matches {
		_ = os.RemoveAll(m)
	}
}

// pluginRowStats summarizes one installed plugin's on-disk content for list.
func pluginRowStats(root string, entry config.PluginEntry) (skillCount int, hasInstructions, missing bool) {
	dir := filepath.Join(root, entry.Name)
	if _, err := os.Stat(dir); err != nil {
		return 0, false, true
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "skills")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				skillCount++
			}
		}
	}
	if info, err := os.Stat(filepath.Join(dir, config.PluginAgentsMDName)); err == nil && info.Size() > 0 {
		hasInstructions = true
	}
	return skillCount, hasInstructions, false
}

func listPlugins(cmd *cobra.Command, _ []string) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := pluginsRoot()
	if err != nil {
		return err
	}

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		data, err := json.MarshalIndent(map[string]any{"enabled": registry.Enabled, "dir": root, "plugins": registry.Plugins}, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal plugins: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(registry.Plugins) == 0 {
		fmt.Println("No plugins installed.")
		fmt.Println("Install one with `infer plugins install <owner>/<repo>` (e.g. DietrichGebert/ponytail).")
		return nil
	}

	fmt.Println(listTitle("Installed Plugins"))
	fmt.Println(listField("Directory", root))
	if !registry.Enabled {
		fmt.Println(listHint("plugins are globally disabled (plugins.enabled: false)"))
	}

	tbl := newListTable("Name", "Version", "Enabled", "Skills", "Instructions", "Source")
	for _, p := range registry.Plugins {
		skillCount, hasInstructions, missing := pluginRowStats(root, p)
		name := p.Name
		if missing {
			name = fmt.Sprintf("%s %s", icons.CrossMark, name)
		}
		instructions := statusIcon(hasInstructions)
		if missing {
			instructions = "missing on disk"
		}
		tbl.Row(name, p.Version, statusIcon(p.Enabled), fmt.Sprintf("%d", skillCount), instructions, p.Source)
	}
	fmt.Println(tbl.Render())
	fmt.Println(statusLegend())
	return nil
}

func removePlugin(_ *cobra.Command, args []string) error {
	name := args[0]
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := pluginsRoot()
	if err != nil {
		return err
	}

	dir, err := plugins.Uninstall(name, root)
	if err != nil {
		return err
	}
	if err := registry.DeleteEntry(name); err != nil {
		return err
	}
	fmt.Printf("%s Removed plugin %s (%s)\n", icons.CheckMarkStyle.Render(icons.CheckMark), name, dir)
	return nil
}

func updatePlugins(_ *cobra.Command, args []string) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := pluginsRoot()
	if err != nil {
		return err
	}

	targets := registry.Plugins
	if len(args) == 1 {
		entry, err := registry.ReadEntry(args[0])
		if err != nil {
			return err
		}
		targets = []config.PluginEntry{*entry}
	}
	if len(targets) == 0 {
		fmt.Println("No plugins installed.")
		return nil
	}

	installer := plugins.NewInstaller()
	for _, entry := range targets {
		if err := updateOnePlugin(installer, registry, root, entry); err != nil {
			return fmt.Errorf("updating %s: %w", entry.Name, err)
		}
	}
	return nil
}

func updateOnePlugin(installer *plugins.Installer, registry *config.PluginsConfig, root string, entry config.PluginEntry) error {
	src, err := plugins.ParseSource(entry.Source)
	if err != nil {
		return err
	}
	if entry.Ref != "" {
		src.Ref = entry.Ref
	}

	staging := filepath.Join(root, fmt.Sprintf(".staging-%d-%s", os.Getpid(), entry.Name))
	defer func() { _ = os.RemoveAll(staging) }()

	if _, err := installer.Stage(context.Background(), src, staging); err != nil {
		return err
	}
	res, err := plugins.Inspect(staging, entry.Name)
	if err != nil {
		return err
	}
	if res.Name != entry.Name {
		return fmt.Errorf("source now identifies as %q (was %q) - remove and reinstall to rename", res.Name, entry.Name)
	}
	if err := plugins.Commit(staging, filepath.Join(root, entry.Name), true); err != nil {
		return err
	}

	oldVersion := entry.Version
	entry.Version = res.Version
	if err := registry.UpdateEntry(entry); err != nil {
		return err
	}
	if oldVersion != res.Version && res.Version != "" {
		fmt.Printf("%s Updated %s %s → %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), entry.Name, oldVersion, res.Version)
	} else {
		fmt.Printf("%s Updated %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), entry.Name)
	}
	return nil
}

func setPluginEnabled(name string, enabled bool) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	entry, err := registry.ReadEntry(name)
	if err != nil {
		return err
	}
	entry.Enabled = enabled
	if err := registry.UpdateEntry(*entry); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("%s Plugin %s %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), name, state)
	return nil
}
