package infrastructure

import (
	"context"
	"sync"
	"time"

	uuid "github.com/google/uuid"
	websocket "github.com/gorilla/websocket"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
)

// toolRequests runs extension-initiated tool calls through the standard pipeline:
// the enabled check, the agent's approval policy, execution, and the same
// conversation recording the TUI's direct-exec path does.
type toolRequests struct {
	write     frameWriter
	service   agentdomain.ToolService
	approval  agentdomain.ApprovalPolicy
	repo      convdomain.ConversationRepository
	notifier  agentdomain.UINotifier
	events    agentdomain.EventBridge
	sessionID string

	mu      sync.Mutex
	pending map[string]chan bool
}

func newToolRequests(write frameWriter, deps Deps) *toolRequests {
	return &toolRequests{
		write:     write,
		service:   deps.Tools,
		approval:  deps.Approval,
		repo:      deps.Conversations,
		notifier:  deps.Notifier,
		events:    deps.Events,
		sessionID: deps.SessionID,
		pending:   make(map[string]chan bool),
	}
}

// reset forgets every approval the previous connection was waiting on.
func (t *toolRequests) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pending = make(map[string]chan bool)
}

// run executes one tool_request and writes exactly one tool_result per request
// id. A dead connection drops the write, there is no queuing or replay.
// approval_behaviour (prompt/ipc/block) is ignored: the extension itself is the
// prompt surface.
func (t *toolRequests) run(conn *websocket.Conn, stop chan struct{}, msg extInbound) {
	reply := func(success bool, output, errStr string) {
		t.write(conn, extToolResult{Type: outboundToolResult, ID: msg.ID, Success: success, Output: output, Error: errStr})
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
	if t.approval.ShouldRequireApproval(ctx, &toolCall, true) {
		if !t.awaitApproval(conn, stop, toolCall) {
			reply(false, "", "tool call denied")
			return
		}
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

// awaitApproval sends an approval_request for an extension-initiated tool call
// and blocks until the panel answers, the connection dies, or the approval times
// out. Anything but an explicit approve is a denial.
func (t *toolRequests) awaitApproval(conn *websocket.Conn, stop chan struct{}, toolCall sdk.ChatCompletionMessageToolCall) bool {
	requestID := uuid.NewString()
	decision := make(chan bool, 1)
	t.mu.Lock()
	t.pending[requestID] = decision
	t.mu.Unlock()
	t.write(conn, extApprovalRequest{
		Type:      outboundApprovalRequest,
		RequestID: requestID,
		ToolName:  toolCall.Function.Name,
		ToolArgs:  toolCall.Function.Arguments,
	})
	select {
	case approved := <-decision:
		return approved
	case <-stop:
	case <-time.After(constants.ApprovalTimeout):
	}
	t.mu.Lock()
	delete(t.pending, requestID)
	t.mu.Unlock()
	return false
}

// resolveApproval answers an approval_response that belongs to an
// extension-initiated tool_request. It reports false when the id is not ours, so
// the caller can fall through to the agent-approval path.
func (t *toolRequests) resolveApproval(conn *websocket.Conn, requestID, action string) bool {
	t.mu.Lock()
	decision, ok := t.pending[requestID]
	delete(t.pending, requestID)
	t.mu.Unlock()
	if !ok {
		return false
	}
	decision <- action == approvalActionApprove
	t.write(conn, extApprovalResolved{Type: outboundApprovalResolved, RequestID: requestID})
	return true
}

// record makes an extension-initiated tool call part of the conversation,
// mirroring the TUI's direct-exec path: persist the assistant tool_call and the
// tool result, refresh the TUI history, and stream the call and result to the
// panel. Denied and disabled calls never reach here, nothing ran.
func (t *toolRequests) record(toolCall sdk.ChatCompletionMessageToolCall, result *agentdomain.ToolExecutionResult) {
	if result.ToolCallID == "" {
		result.ToolCallID = toolCall.ID
	}
	now := time.Now()
	assistantEntry, toolEntry := convdomain.NewToolCallEntries(toolCall, result, t.repo.FormatToolResultForLLM(result), now)
	t.mu.Lock()
	_ = t.repo.AddMessage(assistantEntry)
	_ = t.repo.AddMessage(toolEntry)
	t.mu.Unlock()

	completed := agentdomain.ToolExecutionCompletedEvent{
		SessionID:     t.sessionID,
		RequestID:     toolCall.ID,
		Timestamp:     now,
		TotalExecuted: 1,
		Results:       []*agentdomain.ToolExecutionResult{result},
	}
	if result.Success {
		completed.SuccessCount = 1
	} else {
		completed.FailureCount = 1
	}
	t.notifier.Notify(completed)
	t.events.Publish(agentdomain.ChatCompleteEvent{
		RequestID: toolCall.ID,
		Timestamp: now,
		ToolCalls: []sdk.ChatCompletionMessageToolCall{toolCall},
	})
	t.events.Publish(completed)
}
