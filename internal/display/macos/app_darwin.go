//go:build darwin

package macos

import (
	"context"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// macosAppProvider implements display.AppProvider using NSWorkspace via the
// cgo bridge in client_darwin.go. It does NOT require accessibility
// permissions (those are only needed for UI element inspection via AX APIs;
// NSWorkspace enumeration and activation work without them).
type macosAppProvider struct{}

var _ display.AppProvider = (*macosAppProvider)(nil)

func (macosAppProvider) ListRunning(ctx context.Context) ([]domain.Application, error) {
	return listRunningApps()
}

func (macosAppProvider) Activate(ctx context.Context, id string) error {
	return activateApp(id)
}

func (macosAppProvider) GetFocused(ctx context.Context) (*domain.Application, error) {
	return frontmostApp()
}

func init() {
	display.RegisterAppProvider(macosAppProvider{})
	logger.Debug("registered macOS app provider")
}
