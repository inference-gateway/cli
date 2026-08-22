package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	huh "charm.land/huh/v2"
	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	plugins "github.com/inference-gateway/cli/internal/plugins"
)

type command struct {
	state    *runtime.State
	renderer *output.Renderer
}

// NewCommand constructs the plugins command tree.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	c := &command{state: state, renderer: renderer}
	pluginsCmd := &cobra.Command{
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
	pluginsInstallCmd := &cobra.Command{
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
		RunE: c.installPlugin,
	}
	pluginsListCmd := &cobra.Command{Use: "list", Short: "List installed plugins", RunE: c.listPlugins}
	pluginsRemoveCmd := &cobra.Command{Use: "remove <name>", Short: "Remove an installed plugin", Args: cobra.ExactArgs(1), RunE: c.removePlugin}
	pluginsUpdateCmd := &cobra.Command{Use: "update [<name>]", Short: "Re-fetch one or all plugins from their install source", Args: cobra.MaximumNArgs(1), RunE: c.updatePlugins}
	pluginsEnableCmd := &cobra.Command{
		Use: "enable <name>", Short: "Enable an installed plugin", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return c.setPluginEnabled(args[0], true) },
	}
	pluginsDisableCmd := &cobra.Command{
		Use: "disable <name>", Short: "Disable an installed plugin (skills unloaded, instructions removed)", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return c.setPluginEnabled(args[0], false) },
	}
	pluginsEnableHooksCmd := &cobra.Command{
		Use:   "enable-hooks <name>",
		Short: "Enable command hooks for a plugin",
		Long: `Enable the hooks.yaml shipped by a plugin. Plugin hooks are disabled by
default and must be explicitly enabled per plugin. The master hooks.enabled
switch (or INFER_HOOKS_ENABLED) still applies on top: plugin hooks only run
when both the master switch and the per-plugin flag are true.`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return c.setPluginHooksEnabled(args[0], true) },
	}
	pluginsDisableHooksCmd := &cobra.Command{
		Use: "disable-hooks <name>", Short: "Disable command hooks for a plugin", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error { return c.setPluginHooksEnabled(args[0], false) },
	}

	pluginsCmd.AddCommand(pluginsInstallCmd, pluginsListCmd, pluginsRemoveCmd, pluginsUpdateCmd, pluginsEnableCmd, pluginsDisableCmd, pluginsEnableHooksCmd, pluginsDisableHooksCmd)

	pluginsInstallCmd.Flags().String("ref", "", "Git ref (branch, tag, or commit) to install from")
	pluginsInstallCmd.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")
	pluginsInstallCmd.Flags().Bool("overwrite", false, "Replace an already-installed plugin with the same name")
	pluginsListCmd.Flags().StringP("format", "f", "text", "Output format (text|json)")

	return pluginsCmd
}

// pluginsRoot resolves the plugins storage root from the effective config.
func (c *command) pluginsRoot() (string, error) {
	if cfg := c.state.Config(); cfg != nil {
		return cfg.Plugins.ResolveDir()
	}
	return config.PluginsConfig{}.ResolveDir()
}

// loadPluginsRegistry loads ~/.infer/plugins.yaml; the file is created on first save.
func loadPluginsRegistry() (*config.PluginsConfig, error) {
	registry, err := config.LoadPlugins(runtime.PluginsConfigPath())
	if err != nil {
		return nil, fmt.Errorf("failed to load plugins registry: %w", err)
	}
	return registry, nil
}

func (c *command) installPlugin(cmd *cobra.Command, args []string) error {
	src, err := plugins.ParseSource(args[0])
	if err != nil {
		return err
	}
	if ref, _ := cmd.Flags().GetString("ref"); ref != "" {
		src.Ref = ref
	}
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	yes, _ := cmd.Flags().GetBool("yes")

	root, err := c.pluginsRoot()
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

	c.printInstallSummary(res, src)
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

	fmt.Printf("%s Installed plugin %s to %s\n", c.renderer.StatusIcon(true), res.Name, finalDir)
	c.printPostInstallNotes()
	return nil
}

// printInstallSummary tells the user what the plugin will activate before they confirm.
func (c *command) printInstallSummary(res *plugins.InstallResult, src plugins.Source) {
	fmt.Println(c.renderer.Title(fmt.Sprintf("Plugin: %s", res.Name)))
	if res.Version != "" {
		fmt.Println(c.renderer.Field("Version", res.Version))
	}
	if res.Description != "" {
		fmt.Println(c.renderer.Field("Description", res.Description))
	}
	fmt.Println(c.renderer.Field("Source", src.Raw))

	if len(res.Skills) > 0 {
		names := make([]string, 0, len(res.Skills))
		for _, sk := range res.Skills {
			names = append(names, sk.Name)
		}
		fmt.Println(c.renderer.Field(fmt.Sprintf("Skills (%d)", len(res.Skills)), strings.Join(names, ", ")))
	}
	for _, skErr := range res.SkillErrors {
		fmt.Println(c.renderer.Hint(fmt.Sprintf("skipped invalid skill %s: %s", skErr.Path, skErr.Reason)))
	}
	if res.HasInstructions {
		fmt.Println(c.renderer.Field("Instructions", fmt.Sprintf("AGENTS.md (%d chars) - injected into EVERY system prompt while enabled", res.InstructionsLen)))
	}
	if res.HasHooks {
		parts := make([]string, 0, len(res.Hooks))
		for _, h := range res.Hooks {
			parts = append(parts, fmt.Sprintf("%s (%s)", h.Name, h.Hook))
		}
		fmt.Println(c.renderer.Field(fmt.Sprintf("Hooks (%d)", len(res.Hooks)), strings.Join(parts, ", ")))
		fmt.Println(c.renderer.Hint("hooks are disabled by default - use `infer plugins enable-hooks <name>` to opt in"))
	}
	if len(res.Unsupported) > 0 {
		parts := make([]string, 0, len(res.Unsupported))
		for label, n := range res.Unsupported {
			parts = append(parts, fmt.Sprintf("%s/ (%d files)", label, n))
		}
		fmt.Println(c.renderer.Hint(fmt.Sprintf("detected but ignored: %s - infer does not execute plugin code", strings.Join(parts, ", "))))
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
	var ok bool
	if err := huh.NewConfirm().Title("Proceed with installation?").Value(&ok).Run(); err != nil || !ok {
		return fmt.Errorf("installation aborted")
	}
	return nil
}

func (c *command) printPostInstallNotes() {
	cfg := c.state.Config()
	if cfg == nil {
		return
	}
	if !cfg.Plugins.Enabled {
		fmt.Println(c.renderer.Hint("note: plugins are globally disabled (plugins.enabled: false / INFER_PLUGINS_ENABLED=false)"))
	}
	if !cfg.Agent.Skills.Enabled {
		fmt.Println(c.renderer.Hint("note: agent.skills.enabled is false - plugin skills will not load (instructions still inject)"))
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

func (c *command) listPlugins(cmd *cobra.Command, _ []string) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := c.pluginsRoot()
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

	fmt.Println(c.renderer.Title("Installed Plugins"))
	fmt.Println(c.renderer.Field("Directory", root))
	if !registry.Enabled {
		fmt.Println(c.renderer.Hint("plugins are globally disabled (plugins.enabled: false)"))
	}

	tbl := c.renderer.NewListTable("Name", "Version", "Enabled", "Skills", "Instructions", "Source")
	for _, p := range registry.Plugins {
		skillCount, hasInstructions, missing := pluginRowStats(root, p)
		name := p.Name
		if missing {
			name = fmt.Sprintf("%s %s", c.renderer.StatusIcon(false), name)
		}
		instructions := c.renderer.StatusIcon(hasInstructions)
		if missing {
			instructions = "missing on disk"
		}
		tbl.Row(name, p.Version, c.renderer.StatusIcon(p.Enabled), fmt.Sprintf("%d", skillCount), instructions, p.Source)
	}
	fmt.Println(tbl.Render())
	fmt.Println(c.renderer.StatusLegend())
	return nil
}

func (c *command) removePlugin(_ *cobra.Command, args []string) error {
	name := args[0]
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := c.pluginsRoot()
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
	fmt.Printf("%s Removed plugin %s (%s)\n", c.renderer.StatusIcon(true), name, dir)
	return nil
}

func (c *command) updatePlugins(_ *cobra.Command, args []string) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	root, err := c.pluginsRoot()
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
		if err := c.updateOnePlugin(installer, registry, root, entry); err != nil {
			return fmt.Errorf("updating %s: %w", entry.Name, err)
		}
	}
	return nil
}

func (c *command) updateOnePlugin(installer *plugins.Installer, registry *config.PluginsConfig, root string, entry config.PluginEntry) error {
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
		fmt.Printf("%s Updated %s %s → %s\n", c.renderer.StatusIcon(true), entry.Name, oldVersion, res.Version)
	} else {
		fmt.Printf("%s Updated %s\n", c.renderer.StatusIcon(true), entry.Name)
	}
	return nil
}

func (c *command) setPluginField(name string, enabled bool, fieldFn func(*config.PluginEntry, bool), label string) error {
	registry, err := loadPluginsRegistry()
	if err != nil {
		return err
	}
	entry, err := registry.ReadEntry(name)
	if err != nil {
		return err
	}
	fieldFn(entry, enabled)
	if err := registry.UpdateEntry(*entry); err != nil {
		return err
	}
	state := "enabled"
	if !enabled {
		state = "disabled"
	}
	fmt.Printf("%s %s %s\n", c.renderer.StatusIcon(true), label, state)
	return nil
}

func (c *command) setPluginEnabled(name string, enabled bool) error {
	return c.setPluginField(name, enabled, func(e *config.PluginEntry, v bool) { e.Enabled = v }, "Plugin "+name)
}

func (c *command) setPluginHooksEnabled(name string, enabled bool) error {
	return c.setPluginField(name, enabled, func(e *config.PluginEntry, v bool) { e.HooksEnabled = v }, "Plugin hooks for "+name)
}
