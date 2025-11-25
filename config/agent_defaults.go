package config

import (
	"fmt"
	"net"
)

// AgentDefaults contains default configuration for a known agent type
type AgentDefaults struct {
	URL          string
	ArtifactsURL string
	OCI          string
	Run          bool
	Model        string
	Environment  map[string]string
}

// agentBaseDefaults is a template for agent configurations with base ports
// The actual ports will be determined dynamically to avoid collisions
var agentBaseDefaults = map[string]struct {
	BasePort            int
	ArtifactsPortOffset int
	OCI                 string
	Run                 bool
	Model               string
}{
	"browser-agent": {
		BasePort:            8083,
		ArtifactsPortOffset: 1,
		OCI:                 "ghcr.io/inference-gateway/browser-agent:latest",
		Run:                 true,
		Model:               "deepseek/deepseek-chat",
	},
	"mock-agent": {
		BasePort: 8081,
		OCI:      "ghcr.io/inference-gateway/mock-agent:latest",
		Run:      true,
	},
	"google-calendar-agent": {
		BasePort: 8082,
		OCI:      "ghcr.io/inference-gateway/google-calendar-agent:latest",
		Run:      true,
		Model:    "deepseek/deepseek-chat",
	},
	"documentation-agent": {
		BasePort: 8085,
		OCI:      "ghcr.io/inference-gateway/documentation-agent:latest",
		Run:      true,
		Model:    "deepseek/deepseek-chat",
	},
	"n8n-agent": {
		BasePort: 8086,
		OCI:      "ghcr.io/inference-gateway/n8n-agent:latest",
		Run:      true,
		Model:    "deepseek/deepseek-chat",
	},
}

// isPortAvailable checks if a port is available on localhost
func isPortAvailable(port int) bool {
	address := fmt.Sprintf("localhost:%d", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

// findAvailablePort finds the next available port starting from basePort
// It checks up to 100 ports after the base port
func findAvailablePort(basePort int) int {
	for port := basePort; port < basePort+100; port++ {
		if isPortAvailable(port) {
			return port
		}
	}
	// If no port is available in the range, return the base port
	// The user will get an error when trying to start the agent
	return basePort
}

// GetAgentDefaults returns the default configuration for a known agent
// with dynamically assigned available ports to avoid collisions.
// Returns nil if the agent is not known.
func GetAgentDefaults(name string) *AgentDefaults {
	if template, ok := agentBaseDefaults[name]; ok {
		mainPort := findAvailablePort(template.BasePort)

		defaults := &AgentDefaults{
			URL:         fmt.Sprintf("http://localhost:%d", mainPort),
			OCI:         template.OCI,
			Run:         template.Run,
			Model:       template.Model,
			Environment: nil,
		}

		if template.ArtifactsPortOffset > 0 {
			artifactsPort := findAvailablePort(mainPort + template.ArtifactsPortOffset)
			defaults.ArtifactsURL = fmt.Sprintf("http://localhost:%d", artifactsPort)
		}

		return defaults
	}
	return nil
}

// IsKnownAgent returns true if the agent name has default configuration
func IsKnownAgent(name string) bool {
	_, ok := agentBaseDefaults[name]
	return ok
}

// ListKnownAgents returns a list of all known agent names
func ListKnownAgents() []string {
	names := make([]string, 0, len(agentBaseDefaults))
	for name := range agentBaseDefaults {
		names = append(names, name)
	}
	return names
}
