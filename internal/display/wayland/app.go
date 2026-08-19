//go:build linux

package wayland

import (
	"context"
	"os"

	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// waylandAppProvider implements display.AppProvider for Wayland.
// Wayland's security model deliberately hides global window information
// from clients, so we return ErrAppUnsupported to fall through to any
// X11-based provider or produce a clear "not supported on Wayland" error.
type waylandAppProvider struct{}

var _ display.AppProvider = (*waylandAppProvider)(nil)

func (p *waylandAppProvider) ListRunning(ctx context.Context) ([]domain.Application, error) {
	return nil, display.ErrAppUnsupported
}

func (p *waylandAppProvider) Activate(ctx context.Context, id string) error {
	return display.ErrAppUnsupported
}

func (p *waylandAppProvider) GetFocused(ctx context.Context) (*domain.Application, error) {
	return nil, display.ErrAppUnsupported
}

// Close is a no-op for the Wayland stub.
func (p *waylandAppProvider) Close() error {
	return nil
}

func init() {
	// Only register when Wayland is the active display server.
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		display.RegisterAppProvider(&waylandAppProvider{})
	}
}
