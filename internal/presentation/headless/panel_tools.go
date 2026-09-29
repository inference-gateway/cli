package headless

import (
	"context"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	agui "github.com/inference-gateway/cli/internal/protocols/agui"
)

// toolRequests runs panel-initiated tool calls through the standard pipeline:
// the enabled check, the agent's approval policy, execution, and the same
// conversation recording the TUI's direct-exec path does.
type toolRequests struct {
	write    frameWriter
	events   *agui.Run
	service  agentdomain.ToolService
	approval agentdomain.ApprovalPolicy
	repo     convdomain.ConversationRepository

	mu      sync.Mutex
	pending map[string]chan bool
}

func newToolRequests(write frameWriter, events *agui.Run, deps PanelDeps) *toolRequests {
	return &toolRequests{
		write:    write,
		events:   events,
		service:  deps.Tools,
		approval: deps.Approval,
		repo:     deps.Conversations,
		pending:  make(map[string]chan bool),
	}
}

// run executes one tool_request and writes exactly one tool_result per request
// id. approval_behaviour (prompt/ipc/block) is ignored: the panel itself is the
// prompt surface.
func (t *toolRequests) run(msg panelFrame) {
	reply := func(success bool, output, errStr string) {
		t.write(toolResultFrame{Type: outboundToolResult, ID: msg.ID, Success: success, Output: output, Error: errStr})
	}

	if !t.service.IsToolEnabled(msg.ToolName) {
		reply(false, "", "unknown or disabled tool: "+msg.ToolName)
		return
	}

	toolCall := sdk.ChatCompletionMessageToolCall{
		ID:   msg.ID,
		Type: sdk.Function,
		Function: sdk.ChatCompletionMessageToolCallFunction{
			Name:      msg.ToolName,
			Arguments: msg.ToolArgs,
		},
	}

	ctx := context.Background()
	if t.approval.ShouldRequireApproval(ctx, &toolCall, true) && !t.awaitApproval(toolCall) {
		reply(false, "", "tool call denied")
		return
	}

	result, err := t.service.ExecuteToolDirect(agentdomain.WithToolApproved(ctx), toolCall.Function)
	if err != nil {
		result = &agentdomain.ToolExecutionResult{ToolName: msg.ToolName, ToolCallID: msg.ID, Success: false, Error: err.Error()}
		t.record(toolCall, result)
		reply(false, "", err.Error())
		return
	}
	t.record(toolCall, result)
	reply(result.Success, convdomain.ToolResultOutput(result, t.repo.FormatToolResultForLLM), result.Error)
}

// awaitApproval emits the CUSTOM approval_request every approval uses, keyed by
// the tool_request id, and blocks until an approval_response answers it or the
// approval times out. Anything but an explicit approve is a denial.
func (t *toolRequests) awaitApproval(toolCall sdk.ChatCompletionMessageToolCall) bool {
	decision := make(chan bool, 1)
	t.mu.Lock()
	t.pending[toolCall.ID] = decision
	t.mu.Unlock()
	t.events.Custom("approval_request", ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   toolCall.Function.Name,
		ToolArgs:   toolCall.Function.Arguments,
		ToolCallID: toolCall.ID,
	})
	select {
	case approved := <-decision:
		return approved
	case <-time.After(constants.ApprovalTimeout):
	}
	t.mu.Lock()
	delete(t.pending, toolCall.ID)
	t.mu.Unlock()
	return false
}

// resolve answers the pending approval of a panel tool_request. It reports
// false when toolCallID is not one, so the caller hands the response to the
// running turn instead.
func (t *toolRequests) resolve(toolCallID string, approved bool) bool {
	t.mu.Lock()
	decision, ok := t.pending[toolCallID]
	delete(t.pending, toolCallID)
	t.mu.Unlock()
	if ok {
		decision <- approved
	}
	return ok
}

// record makes a panel-initiated tool call part of the conversation, mirroring
// the TUI's direct-exec path: persist the assistant tool_call and the tool
// result. Denied and disabled calls never reach here, nothing ran.
func (t *toolRequests) record(toolCall sdk.ChatCompletionMessageToolCall, result *agentdomain.ToolExecutionResult) {
	if result.ToolCallID == "" {
		result.ToolCallID = toolCall.ID
	}
	assistantEntry, toolEntry := convdomain.NewToolCallEntries(toolCall, result, t.repo.FormatToolResultForLLM(result), time.Now())
	t.mu.Lock()
	defer t.mu.Unlock()
	_ = t.repo.AddMessage(assistantEntry)
	_ = t.repo.AddMessage(toolEntry)
}
