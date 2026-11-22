package toolcoordinator

import (
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

// writeSubagentApprovalSidecar records that this chat is blocked on a tool
// approval, so a parent watching an interactive subagent can surface it and relay
// the decision (see scheddomain.EnvSubagentApprovalFile / ApproveSubagent). It is a
// no-op unless this process is an interactive subagent (the env var is set only
// by the Agent tool's buildChatPaneCommand), so top-level chats are unaffected.
func writeSubagentApprovalSidecar(toolCall sdk.ChatCompletionMessageToolCall) {
	path := os.Getenv(scheddomain.EnvSubagentApprovalFile)
	if path == "" {
		return
	}
	approval := scheddomain.SubagentApprovalFile{
		Awaiting: true,
		Summary:  subagentApprovalSummary(toolCall),
	}
	if err := scheddomain.WriteSubagentApprovalFile(path, approval); err != nil {
		logger.Warn("failed to write the subagent approval file", "path", path, "error", err)
	}
}

// clearSubagentApprovalSidecar removes the approval sidecar once the prompt
// resolves. No-op when not running as an interactive subagent.
func clearSubagentApprovalSidecar() {
	path := os.Getenv(scheddomain.EnvSubagentApprovalFile)
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// subagentApprovalSummary renders a concise one-liner of the pending tool call
// (name + arguments, bounded) for the parent to show the user.
func subagentApprovalSummary(tc sdk.ChatCompletionMessageToolCall) string {
	name := tc.Function.Name
	args := strings.TrimSpace(tc.Function.Arguments)
	if args == "" || args == "{}" {
		return name
	}
	const maxArgs = 300
	if len(args) > maxArgs {
		args = args[:maxArgs] + "…"
	}
	return name + " " + args
}
