package domain

// ConfigService provides configuration-related functionality
type ConfigService interface {
	// Tool approval configuration
	IsApprovalRequired(toolName string) bool

	// Debug and output configuration
	GetOutputDirectory() string

	// Gateway configuration
	GetGatewayURL() string
	GetAPIKey() string
	GetTimeout() int

	// Chat configuration
	GetSystemPrompt() string
	GetDefaultModel() string

	// Sandbox configuration
	GetSandboxDirectories() []string
	GetProtectedPaths() []string

	// Gateway middleware configuration
	ShouldSkipA2AToolOnClient() bool
	ShouldSkipMCPToolOnClient() bool
}
