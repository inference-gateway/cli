package tools

import (
	"embed"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// Names of the agent's built-in tools, as their manifests declare them.
const (
	ToolAgent               = "Agent"
	ToolApproveSubagent     = "ApproveSubagent"
	ToolAskUserQuestion     = "AskUserQuestion"
	ToolBash                = "Bash"
	ToolBashOutput          = "BashOutput"
	ToolCloseSubagent       = "CloseSubagent"
	ToolCreateAvatar        = "CreateAvatar"
	ToolDelete              = "Delete"
	ToolEdit                = "Edit"
	ToolGetSubagentResult   = "GetSubagentResult"
	ToolGrep                = "Grep"
	ToolImageDecode         = "ImageDecode"
	ToolImageEdit           = "ImageEdit"
	ToolImageGeneration     = "ImageGeneration"
	ToolImageVariation      = "ImageVariation"
	ToolKillShell           = "KillShell"
	ToolListShells          = "ListShells"
	ToolListSubagents       = "ListSubagents"
	ToolMemory              = "Memory"
	ToolMultiEdit           = "MultiEdit"
	ToolRead                = "Read"
	ToolReadSubagentScreen  = "ReadSubagentScreen"
	ToolRequestApproval     = "RequestApproval"
	ToolRequestPlanApproval = "RequestPlanApproval"
	ToolSchedule            = "Schedule"
	ToolSendSubagentInput   = "SendSubagentInput"
	ToolTextToMusic         = "TextToMusic"
	ToolTextToSFX           = "TextToSFX"
	ToolTextToSpeech        = "TextToSpeech"
	ToolTextToVideo         = "TextToVideo"
	ToolTodoWrite           = "TodoWrite"
	ToolTree                = "Tree"
	ToolWait                = "Wait"
	ToolWebFetch            = "WebFetch"
	ToolWebSearch           = "WebSearch"
	ToolWrite               = "Write"
)

//go:embed *.yaml
var toolManifestFiles embed.FS

// toolManifests define the agent's built-in tools: the schema the model sees,
// the default description and the approval, read-only and plan-mode policy.
// Change a tool by editing the <Name>.yaml next to its implementation.
var toolManifests = agentdomain.MustLoadToolManifests(toolManifestFiles, ".")
