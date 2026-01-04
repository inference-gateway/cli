//go:build !darwin

package cmd

import (
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// FloatingWindowManager is the platform-specific interface for the floating window
type FloatingWindowManager interface {
	Shutdown() error
}

// noopFloatingWindowManager is a no-op implementation for non-darwin platforms
type noopFloatingWindowManager struct{}

func (n *noopFloatingWindowManager) Shutdown() error {
	return nil
}

// initFloatingWindow returns a no-op manager on non-darwin platforms
func initFloatingWindow(config *config.Config, stateManager domain.StateManager) (FloatingWindowManager, error) {
	return &noopFloatingWindowManager{}, nil
}
