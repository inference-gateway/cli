package cmd

import "github.com/inference-gateway/cli/internal/domain"

// GetVersionInfo returns the current version information
func GetVersionInfo() domain.VersionInfo {
	return domain.VersionInfo{
		Version: version,
		Commit:  commit,
		Date:    date,
	}
}
