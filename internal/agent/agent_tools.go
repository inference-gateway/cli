package agent

import (
	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// executeTools executes all tools in parallel (runs in background goroutine)
func (a *EventDrivenAgent) executeTools() {
	defer a.wg.Done()

	logger.Debug("executing tools", "tool_count", len(a.currentToolCalls))

	toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
	for _, tc := range a.currentToolCalls {
		toolCallsSlice = append(toolCallsSlice, tc)
		logger.Debug("executing tool", "tool", tc.Function.Name, "id", tc.ID)
	}

	logger.Debug("running tools in parallel...")
	toolResults := a.service.executeToolCallsParallel(a.agentCtx.Ctx, toolCallsSlice, a.eventPublisher, a.req.IsChatMode)
	logger.Debug("tool execution completed", "result_count", len(toolResults))

	stop := a.service.handleToolResults(toolResults, a.agentCtx.Conversation, a.eventPublisher, a.req)

	failed := domain.AnyToolFailed(toolResults)
	a.mu.Lock()
	a.agentCtx.LastToolFailed = failed
	if !stop {
		a.agentCtx.HasToolResults = true
	}
	a.mu.Unlock()

	logger.Debug("emitting tools completed event", "stop", stop)
	a.events <- domain.ToolsCompletedEvent{Results: toolResults, Stop: stop}
}
