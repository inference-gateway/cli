package version

import (
	tui "github.com/inference-gateway/cli/internal/presentation/tui"
)

// GetVersionInfo returns the current version information
func GetVersionInfo() tui.VersionInfo {
	return tui.VersionInfo{
		Version: version,
	}
}
