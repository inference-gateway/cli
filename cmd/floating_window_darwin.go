//go:build darwin

package cmd

import (
	"fmt"

	config "github.com/inference-gateway/cli/config"
	macos "github.com/inference-gateway/cli/internal/display/macos"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// FloatingWindowManager is the platform-specific interface for the floating window
type FloatingWindowManager interface {
	Shutdown() error
}

// initFloatingWindow initializes the floating window manager if enabled
func initFloatingWindow(config *config.Config, stateManager domain.StateManager) (FloatingWindowManager, error) {
	logger.Info("Checking floating window conditions",
		"computer_use_enabled", config.ComputerUse.Enabled,
		"floating_window_enabled", config.ComputerUse.FloatingWindow.Enabled)

	if !config.ComputerUse.Enabled || !config.ComputerUse.FloatingWindow.Enabled {
		return nil, nil
	}

	logger.Info("Initializing floating window manager")
	eventBridge := macos.NewEventBridge()
	stateManager.SetEventBridge(eventBridge)

	floatingWindowMgr, err := macos.NewFloatingWindowManager(config, eventBridge, stateManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create floating window manager: %w", err)
	}

	return floatingWindowMgr, nil
}
