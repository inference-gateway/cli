package computer

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Names of the computer tools, as their manifests declare them.
const (
	ToolComputer       = "Computer"
	ToolGetLatestFrame = "GetLatestFrame"
	ToolRecordStart    = "RecordStart"
	ToolRecordStop     = "RecordStop"
)

//go:embed tools/*.yaml
var toolManifestFiles embed.FS

// toolManifests define the computer tools: the schema the model sees, the
// default description and the approval, read-only and plan-mode policy.
// Change a tool by editing its tools/<Name>.yaml.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, "tools")

// ToolNames returns the names of the computer tools, whether or not config enables them,
// so a custom tool can never take one.
func ToolNames() []string {
	return toolManifests.Names()
}
