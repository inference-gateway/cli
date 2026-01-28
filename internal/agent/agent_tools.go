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

	// Convert map to slice
	toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(a.currentToolCalls))
	for _, tc := range a.currentToolCalls {
		toolCallsSlice = append(toolCallsSlice, tc)
		logger.Debug("executing tool", "tool", tc.Function.Name, "id", tc.Id)
	}

	logger.Debug("Running tools in parallel...")
	toolResults := a.service.executeToolCallsParallel(a.agentCtx.Ctx, toolCallsSlice, a.eventPublisher, a.req.IsChatMode)
	logger.Debug("tool execution completed", "result_count", len(toolResults))

	// Handle tool results
	if a.service.handleToolResults(toolResults, a.agentCtx.Conversation, a.eventPublisher, a.req) {
		logger.Debug("Tool results indicated stop (user rejection or error)")
		_ = a.stateMachine.Transition(a.agentCtx, domain.StateStopped)
		return
	}

	// Mark that we have tool results
	a.mu.Lock()
	a.agentCtx.HasToolResults = true
	a.mu.Unlock()

	// Emit tools completed event
	logger.Debug("emitting tools completed event")
	a.events <- domain.ToolsCompletedEvent{Results: toolResults}
}
