package tools

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

//go:embed *.yaml
var toolManifestFiles embed.FS

// toolManifests define the agent's built-in tools: the schema the model sees,
// the default description and the approval, read-only and plan-mode policy.
// Change a tool by editing the <Name>.yaml next to its implementation.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, ".")
