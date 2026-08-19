package display

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// Registry manages display server providers and handles display detection
type Registry struct {
	providers    []Provider
	appProviders []AppProvider
	mu           sync.RWMutex
}

var (
	globalRegistry = &Registry{
		providers:    make([]Provider, 0),
		appProviders: make([]AppProvider, 0),
	}
)

// Register adds a display server provider to the global registry
// This is typically called from init() functions in display-specific packages
func Register(provider Provider) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.providers = append(globalRegistry.providers, provider)
}

// DetectDisplay returns the first available display server provider
// Priority is determined by registration order (first registered has highest priority)
func DetectDisplay() (Provider, error) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	for _, p := range globalRegistry.providers {
		if p.IsAvailable() {
			return p, nil
		}
	}

	return nil, fmt.Errorf("no compatible display server detected (tried %d providers)", len(globalRegistry.providers))
}

// RegisterAppProvider adds an AppProvider to the global registry.
// Called from init() in platform-specific packages (macOS, X11, Wayland).
func RegisterAppProvider(ap AppProvider) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.appProviders = append(globalRegistry.appProviders, ap)
}

// DetectAppProvider returns the first available AppProvider for the current
// platform. Providers are probed in registration order until one succeeds
// (does not return ErrAppUnsupported). Returns ErrNoAppProvider when no
// platform is available.
func DetectAppProvider() (AppProvider, error) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	for _, ap := range globalRegistry.appProviders {
		// Probe by calling ListRunning; if the platform is available it
		// succeeds (possibly with an empty list). If it returns
		// ErrAppUnsupported we skip it.
		_, err := ap.ListRunning(context.Background())
		if err == nil || !errors.Is(err, ErrAppUnsupported) {
			return ap, nil
		}
	}

	return nil, fmt.Errorf("no application provider available on this platform (tried %d providers): %w", len(globalRegistry.appProviders), ErrNoAppProvider)
}

// ErrNoAppProvider is returned when no application provider is available.
var ErrNoAppProvider = fmt.Errorf("no application provider")
