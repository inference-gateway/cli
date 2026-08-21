package cmd

import (
	ui "github.com/inference-gateway/cli/internal/ui"
)

// GetVersionInfo returns the current version information
func GetVersionInfo() ui.VersionInfo {
	return ui.VersionInfo{
		Version: version,
	}
}
