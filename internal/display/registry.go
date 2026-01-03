package display

import (
	"fmt"
	"sync"
)

// Registry manages display server providers and handles display detection
type Registry struct {
	providers []Provider
	mu        sync.RWMutex
}

var (
	globalRegistry = &Registry{
		providers: make([]Provider, 0),
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

// GetAllProviders returns all registered providers
func GetAllProviders() []Provider {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	providers := make([]Provider, len(globalRegistry.providers))
	copy(providers, globalRegistry.providers)
	return providers
}

// GetProvider returns a specific provider by display server name, or nil if not found
func GetProvider(displayName string) Provider {
	globalRegistry.mu.RLock()
	defer globalRegistry.mu.RUnlock()

	for _, p := range globalRegistry.providers {
		if p.GetDisplayInfo().Name == displayName {
			return p
		}
	}

	return nil
}

// ClearProviders removes all registered providers (primarily for testing)
func ClearProviders() {
	globalRegistry.mu.Lock()
	defer globalRegistry.mu.Unlock()
	globalRegistry.providers = make([]Provider, 0)
}
