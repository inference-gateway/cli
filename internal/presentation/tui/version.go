package tui

// VersionInfo contains build-time version information plus the detected
// version of the gateway serving this session (empty when unknown).
type VersionInfo struct {
	Version        string
	GatewayVersion string
}
