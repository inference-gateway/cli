package browser

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Names of the browser tools, as their manifests declare them.
const (
	ToolClick      = "BrowserClick"
	ToolNavigate   = "BrowserNavigate"
	ToolRead       = "BrowserRead"
	ToolScreenshot = "BrowserScreenshot"
	ToolTabs       = "BrowserTabs"
	ToolType       = "BrowserType"
)

//go:embed tools/*.yaml
var toolManifestFiles embed.FS

// toolManifests define the browser tools: the schema the model sees, the
// default description and the approval, read-only and plan-mode policy.
// Change a tool by editing its tools/<Name>.yaml.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, "tools")
