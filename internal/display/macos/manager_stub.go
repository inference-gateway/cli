//go:build !darwin

package macos

import (
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// FloatingWindowManager stub for non-macOS platforms
type FloatingWindowManager struct {
	enabled bool
}

// NewFloatingWindowManager returns a disabled manager on non-macOS platforms
func NewFloatingWindowManager(
	cfg *config.Config,
	eventBridge *EventBridge,
	stateManager domain.StateManager,
	agentService domain.AgentService,
) (*FloatingWindowManager, error) {
	return &FloatingWindowManager{enabled: false}, nil
}

// Shutdown is a no-op on non-macOS platforms
func (mgr *FloatingWindowManager) Shutdown() error {
	return nil
}
