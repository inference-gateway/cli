package display

import (
	"fmt"
	"sync"
)

// Registry manages display server providers and handles display detection
type Registry struct {
	providers    []Provider
	appProviders []AppProvider
	axProviders  []AXProvider
	mu           sync.RWMutex
}

var (
	globalRegistry = &Registry{
		providers:    make([]Provider, 0),
		appProviders: make([]AppProvider, 0),
		axProviders:  make([]AXProvider, 0),
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

// DetectAppProvider returns the first registered AppProvider. Platform
// packages only register a provider when their display server is actually
// available, so registration order is the preference order. Returns
// ErrNoAppProvider when none is available (headless, or Wayland).
func DetectAppProvider() (AppProvider, error) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	if len(globalRegistry.appProviders) == 0 {
		return nil, ErrNoAppProvider
	}
	return globalRegistry.appProviders[0], nil
}

// ErrNoAppProvider is returned when no application provider is available.
var ErrNoAppProvider = fmt.Errorf("no application provider")

// RegisterAXProvider adds an AXProvider to the global registry.
// Called from init() in platform-specific packages (currently macOS only).
func RegisterAXProvider(ax AXProvider) {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.axProviders = append(globalRegistry.axProviders, ax)
}

// DetectAXProvider returns the first registered AXProvider. Platforms
// without an accessibility backend register nothing, so ErrNoAXProvider is
// the graceful-degradation signal (fall back to the vision pipeline).
func DetectAXProvider() (AXProvider, error) {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	if len(globalRegistry.axProviders) == 0 {
		return nil, ErrNoAXProvider
	}
	return globalRegistry.axProviders[0], nil
}
