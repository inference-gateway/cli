package shortcuts

import (
	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// NewMetadataRegistry mirrors the chat TUI's shortcut registry with nil
// dependencies, for surfaces that only read name, description and usage - the
// channel command menu, `infer shortcuts list`. Built-in shortcuts must never
// be Executed from it; custom shortcuts guard their own nil dependencies.
func NewMetadataRegistry(cfg *config.Config) *Registry {
	reg := NewRegistry()

	reg.Register(NewClearShortcut(nil, nil))
	reg.Register(NewCompactShortcut(nil))
	reg.Register(NewCopyShortcut(nil, nil))
	reg.Register(NewContextShortcut(nil, nil, nil))
	reg.Register(NewCostShortcut(nil))
	reg.Register(NewExitShortcut())
	reg.Register(NewSwitchShortcut(nil))
	reg.Register(NewThemeShortcut(nil))
	reg.Register(NewToolsShortcut())
	reg.Register(NewHelpShortcut(reg))
	reg.Register(NewDiffShortcut())
	reg.Register(NewExplorerShortcut())
	reg.Register(NewReleaseNotesShortcut())
	reg.Register(NewStatsShortcut())
	reg.Register(NewTracesShortcut())
	reg.Register(NewConversationSelectShortcut(nil))
	reg.Register(NewNewShortcut(nil, nil))
	reg.Register(NewInstallOpentaskShortcut())
	reg.Register(NewInitShortcut(cfg))
	if cfg.IsA2AToolsEnabled() {
		reg.Register(NewA2ATaskManagementShortcut(cfg))
		reg.Register(NewA2AAgentsShortcut())
	}

	configDirs := config.ConfigLookupDirs()
	if err := reg.LoadCustomShortcuts(configDirs, nil, nil, nil, nil); err != nil {
		logger.Warn("failed to load custom shortcuts", "error", err, "config_dirs", configDirs)
	}
	return reg
}
