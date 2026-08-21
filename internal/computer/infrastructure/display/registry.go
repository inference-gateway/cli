package display

import "fmt"

var provider Provider

// Register sets the display provider (called from init).
func Register(p Provider) {
	provider = p
}

// DetectDisplay returns the display provider when it is available.
func DetectDisplay() (Provider, error) {
	if provider != nil && provider.IsAvailable() {
		return provider, nil
	}
	return nil, fmt.Errorf("no compatible display server detected")
}
