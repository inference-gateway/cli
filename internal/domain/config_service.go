package domain

// ConfigService provides configuration-related functionality
type ConfigService interface {
	// Tool approval configuration
	IsApprovalRequired(toolName string) bool

	// Debug and output configuration
	IsDebugMode() bool
	GetOutputDirectory() string

	// Gateway configuration
	GetGatewayURL() string
	GetAPIKey() string
	GetTimeout() int

	// Chat configuration
	GetSystemPrompt() string
	GetDefaultModel() string
}
