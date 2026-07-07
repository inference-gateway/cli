package plugins

import (
	"fmt"
	"os"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// PluginHookCommandProvider merges the user's hooks.yaml with each enabled
// plugin's hooks.yaml additively: user hooks first, then each enabled plugin's
// hooks in registry order. A plugin's hooks are only included when the plugin
// is enabled, its hooks_enabled flag is true, and the plugin has a hooks.yaml
// on disk. Plugin hooks can never override or remove a user hook.
//
// The master hooks.enabled switch (cfg.Hooks.Enabled) still applies on top:
// when it is false, CommandsDue returns nil regardless of plugin hooks.
type PluginHookCommandProvider struct {
	cfg *config.Config
}

// NewPluginHookCommandProvider creates a provider that merges user hooks with
// enabled plugin hooks. Returns nil when cfg is nil or plugins are disabled.
func NewPluginHookCommandProvider(cfg *config.Config) *PluginHookCommandProvider {
	if cfg == nil {
		return nil
	}
	return &PluginHookCommandProvider{cfg: cfg}
}

// CommandsDue implements domain.HookCommandProvider. It returns user command
// hooks first, then each enabled plugin's hooks (in registry order) whose
// hooks_enabled flag is true and whose hooks.yaml exists on disk. The master
// hooks.enabled switch gates everything: when false, nil is returned.
func (p *PluginHookCommandProvider) CommandsDue(hook domain.HookPoint) []domain.HookCommand {
	if p == nil || p.cfg == nil {
		return nil
	}

	if !p.cfg.Hooks.Enabled {
		return nil
	}

	var due []domain.HookCommand

	due = append(due, p.cfg.Hooks.CommandsDue(hook)...)
	if !p.cfg.Plugins.Enabled {
		return due
	}
	for _, entry := range p.cfg.Plugins.Plugins {
		if !entry.Enabled || !entry.HooksEnabled {
			continue
		}
		pluginHooks := loadPluginHooks(p.cfg.Plugins, entry.Name)
		for _, hc := range pluginHooks {
			if hc.Hook != hook {
				continue
			}
			due = append(due, domain.HookCommand{
				Name:    fmt.Sprintf("%s:%s", entry.Name, hc.Name),
				Command: hc.Command,
				Timeout: time.Duration(hc.Timeout) * time.Second,
			})
		}
	}
	return due
}

// loadPluginHooks reads and parses a plugin's hooks.yaml. Returns nil when the
// file is missing or unreadable (logged at debug level).
func loadPluginHooks(pc config.PluginsConfig, name string) []config.HookCommandConfig {
	path, err := pc.PluginHooksPath(name)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debug("failed to read plugin hooks", "plugin", name, "path", path, "error", err)
		}
		return nil
	}
	var hooksCfg config.HooksConfig
	if err := config.ParseHooksYAML(data, &hooksCfg); err != nil {
		logger.Debug("failed to parse plugin hooks", "plugin", name, "path", path, "error", err)
		return nil
	}
	return hooksCfg.Hooks
}
