package toolcoordinator

import (
	"encoding/json"
	"os"
	"strings"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
)

// writeSubagentApprovalSidecar records that this chat is blocked on a tool
// approval, so a parent watching an interactive subagent can surface it and relay
// the decision (see domain.EnvSubagentApprovalFile / ApproveSubagent). It is a
// no-op unless this process is an interactive subagent (the env var is set only
// by the Agent tool's buildChatPaneCommand), so top-level chats are unaffected.
func writeSubagentApprovalSidecar(toolCall sdk.ChatCompletionMessageToolCall) {
	path := os.Getenv(domain.EnvSubagentApprovalFile)
	if path == "" {
		return
	}
	data, err := json.Marshal(domain.SubagentApprovalFile{
		Awaiting: true,
		Summary:  subagentApprovalSummary(toolCall),
	})
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
}

// clearSubagentApprovalSidecar removes the approval sidecar once the prompt
// resolves. No-op when not running as an interactive subagent.
func clearSubagentApprovalSidecar() {
	path := os.Getenv(domain.EnvSubagentApprovalFile)
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
