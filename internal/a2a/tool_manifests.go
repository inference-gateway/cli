package a2a

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Names of the A2A tools, as their manifests declare them.
const (
	ToolQueryAgent = "A2A_QueryAgent"
	ToolQueryTask  = "A2A_QueryTask"
	ToolSubmitTask = "A2A_SubmitTask"
)

//go:embed tools/*.yaml
var toolManifestFiles embed.FS

// toolManifests define the A2A tools: the schema the model sees, the default
// description and the approval, read-only and plan-mode policy.
// Change a tool by editing its tools/<Name>.yaml.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, "tools")

// ToolNames returns the names of the A2A tools, whether or not config enables them,
// so a custom tool can never take one.
func ToolNames() []string {
	return toolManifests.Names()
}
