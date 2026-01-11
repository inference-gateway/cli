package domain

import "github.com/inference-gateway/cli/config"

// ConfigService provides configuration-related functionality
type ConfigService interface {
	// Tool approval configuration
	IsApprovalRequired(toolName string) bool
	IsBashCommandWhitelisted(command string) bool

	// Debug and output configuration
	GetOutputDirectory() string

	// Gateway configuration
	GetGatewayURL() string
	GetAPIKey() string
	GetTimeout() int

	// Chat configuration
	GetAgentConfig() *config.AgentConfig

	// Sandbox configuration
	GetSandboxDirectories() []string
	GetProtectedPaths() []string

	// Full configuration access
	GetConfig() *config.Config
}
