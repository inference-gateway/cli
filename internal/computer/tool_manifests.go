package computer

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

//go:embed tools/*.yaml
var toolManifestFiles embed.FS

// toolManifests define the computer tools: the schema the model sees, the
// default description and the approval, read-only and plan-mode policy.
// Change a tool by editing its tools/<Name>.yaml.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, "tools")
