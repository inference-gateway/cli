package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// startStreaming implements the LLM streaming logic for the EventDrivenAgent
func (a *EventDrivenAgent) startStreaming() {
	iterationStartTime := time.Now()

	a.agentCtx.Turns++
	a.service.sessionTurns.Add(1)
	a.agentCtx.HasToolResults = false
	a.service.clearToolCallsMap()

	logger.Debug("starting streaming turn",
		"turn", a.agentCtx.Turns,
		"max_turns", a.agentCtx.MaxTurns,
		"conversation_length", len(*a.agentCtx.Conversation))

	if a.agentCtx.Turns > 1 {
		time.Sleep(constants.AgentIterationDelay)
	}

	requestCtx, requestCancel := context.WithTimeout(a.agentCtx.Ctx, time.Duration(a.service.timeoutSeconds)*time.Second)
	defer requestCancel()

	a.service.requestsMux.Lock()
	a.service.activeRequests[a.req.RequestID] = requestCancel
	a.service.requestsMux.Unlock()

	defer func() {
		a.service.requestsMux.Lock()
		delete(a.service.activeRequests, a.req.RequestID)
		a.service.requestsMux.Unlock()
	}()

	a.eventPublisher.publishChatStart()

	if a.agentCtx.Turns == 1 {
		a.service.dispatchHooks(a.agentCtx, domain.HookPreSession)
	}
	a.service.dispatchHooks(a.agentCtx, domain.HookPreStream)

	mode := domain.AgentModeStandard
	if a.service.stateManager != nil {
		mode = a.service.stateManager.GetAgentMode()
	}
	a.availableTools = a.service.toolService.ListToolsForMode(mode)

	client := a.service.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &a.service.maxTokens,
			StreamOptions: &sdk.ChatCompletionStreamOptions{
				IncludeUsage: true,
			},
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		})

	if len(a.availableTools) > 0 {
		client = client.WithTools(&a.availableTools)
	}

	a.service.ensureConversationIntegrity(a.agentCtx.Conversation, a.eventPublisher, a.req.RequestID, false)

	events, err := client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, *a.agentCtx.Conversation)
	if err != nil {
		logger.Error("failed to create stream",
			"error", err,
			"turn", a.agentCtx.Turns,
			"conversationLength", len(*a.agentCtx.Conversation),
			"provider", a.provider)
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     err,
		}
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateError); err != nil {
			logger.Error("failed to transition to Error state after stream failure", "error", err)
		}
		return
	}

	a.processStreamEvents(requestCtx, events, iterationStartTime)
}

// processStreamEvents processes streaming events from the LLM
func (a *EventDrivenAgent) processStreamEvents(
	requestCtx context.Context,
	events <-chan sdk.SSEvent,
	iterationStartTime time.Time,
) {
	var allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk
	var message sdk.Message
	var streamUsage *sdk.CompletionUsage

	for {
		select {
		case <-requestCtx.Done():
			a.handleStreamInterrupted(requestCtx, message)
			return

		case event, ok := <-events:
			if !ok {
				a.finalizeStream(message, allToolCallDeltas, streamUsage, iterationStartTime)
				return
			}

			if usage := a.processStreamEvent(event, &message, &allToolCallDeltas); usage != nil {
				streamUsage = usage
			}
		}
	}
}

// handleStreamInterrupted handles a stream that ended via ctx cancellation -
// either a real timeout (DeadlineExceeded) or user cancellation (Canceled).
// Timeout: publish an error and transition to StateError.
// Cancellation: persist any partial assistant content so the user doesn't
// lose mid-flight output (e.g. a half-written poem when Esc is pressed),
// then return silently - the main event loop owns the StateCancelled
// transition via cancelChan.
func (a *EventDrivenAgent) handleStreamInterrupted(requestCtx context.Context, partial sdk.Message) {
	if requestCtx.Err() == context.DeadlineExceeded {
		logger.Error("stream timeout", "error", requestCtx.Err())
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     fmt.Errorf("stream timed out after %d seconds", a.service.timeoutSeconds),
		}
		if err := a.stateMachine.Transition(a.agentCtx, domain.StateError); err != nil {
			logger.Error("failed to transition to Error state after stream failure", "error", err)
		}
		return
	}

	logger.Debug("stream cancelled", "request_id", a.req.RequestID, "err", requestCtx.Err())
	a.persistPartialAssistantMessage(partial)
}

// persistPartialAssistantMessage saves the partial assistant text + reasoning
// produced before cancellation. Partial tool calls are intentionally dropped:
// a half-streamed tool call can't be executed safely, so attempting to
// preserve it risks malformed arguments. Text content alone is appended to
// both the in-memory conversation and the repo so the next session sees the
// interruption point in history.
func (a *EventDrivenAgent) persistPartialAssistantMessage(partial sdk.Message) {
	content, err := partial.Content.AsMessageContent0()
	if err != nil {
		content = ""
	}

	reasoning := ""
	switch {
	case partial.Reasoning != nil && *partial.Reasoning != "":
		reasoning = *partial.Reasoning
	case partial.ReasoningContent != nil && *partial.ReasoningContent != "":
		reasoning = *partial.ReasoningContent
	}

	if content == "" && reasoning == "" {
		return
	}

	assistantMessage := buildAssistantMessage(sdk.NewMessageContent(content), reasoning, nil)
	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, assistantMessage)

	entry := domain.ConversationEntry{
		Message:          assistantMessage,
		ReasoningContent: reasoning,
		Model:            a.req.Model,
		Time:             time.Now(),
	}
	if err := a.service.conversationRepo.AddMessage(entry); err != nil {
		logger.Error("failed to persist partial assistant message after cancel", "error", err)
	}
}

// processStreamEvent processes a single SSE event from the stream
func (a *EventDrivenAgent) processStreamEvent(
	event sdk.SSEvent,
	message *sdk.Message,
	allToolCallDeltas *[]sdk.ChatCompletionMessageToolCallChunk,
) *sdk.CompletionUsage {
	if event.Event == nil {
		logger.Error("event is nil")
		return nil
	}

	if event.Data == nil {
		return nil
	}

	switch string(*event.Event) {
	case "message_stop", "system_init", "hook_event", "tool_failure", "result_metadata":
		return nil
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		logger.Error("failed to unmarshal chat completion stream response",
			"error", err,
			"raw_data", string(*event.Data))
		return nil
	}

	var streamUsage *sdk.CompletionUsage
	for _, choice := range streamResponse.Choices {
		a.processChoiceDelta(choice, message, allToolCallDeltas)

		if streamResponse.Usage != nil {
			streamUsage = streamResponse.Usage
		}
	}

	return streamUsage
}

// processChoiceDelta processes a single choice delta from the stream response
func (a *EventDrivenAgent) processChoiceDelta(
	choice sdk.ChatCompletionStreamChoice,
	message *sdk.Message,
	allToolCallDeltas *[]sdk.ChatCompletionMessageToolCallChunk,
) {
	a.accumulateReasoning(choice.Delta, message)

	deltaContent := a.accumulateContent(choice.Delta, message)
	reasoning := extractReasoningForEvent(choice.Delta)

	var toolCalls []sdk.ChatCompletionMessageToolCallChunk
	if choice.Delta.ToolCalls != nil {
		toolCalls = *choice.Delta.ToolCalls
	}

	if len(toolCalls) > 0 {
		*allToolCallDeltas = append(*allToolCallDeltas, toolCalls...)
	}

	if deltaContent != "" || reasoning != "" || len(toolCalls) > 0 {
		a.eventPublisher.publishChatChunk(deltaContent, reasoning, toolCalls)
	}
}

// accumulateReasoning accumulates reasoning content from the delta into the message
func (a *EventDrivenAgent) accumulateReasoning(
	delta sdk.ChatCompletionStreamResponseDelta,
	message *sdk.Message,
) {
	if delta.Reasoning != nil && *delta.Reasoning != "" {
		if message.Reasoning == nil {
			message.Reasoning = new(string)
		}
		*message.Reasoning += *delta.Reasoning
	}

	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		if message.ReasoningContent == nil {
			message.ReasoningContent = new(string)
		}
		*message.ReasoningContent += *delta.ReasoningContent
	}
}

// accumulateContent accumulates message content from the delta and returns the delta content
func (a *EventDrivenAgent) accumulateContent(
	delta sdk.ChatCompletionStreamResponseDelta,
	message *sdk.Message,
) string {
	deltaContent := delta.Content
	if deltaContent != "" {
		currentContent, err := message.Content.AsMessageContent0()
		if err != nil {
			currentContent = ""
		}
		message.Content = sdk.NewMessageContent(currentContent + deltaContent)
	}
	return deltaContent
}

// extractReasoningForEvent extracts reasoning content from a delta for event publishing
func extractReasoningForEvent(delta sdk.ChatCompletionStreamResponseDelta) string {
	if delta.Reasoning != nil && *delta.Reasoning != "" {
		return *delta.Reasoning
	}
	if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
		return *delta.ReasoningContent
	}
	return ""
}

// buildAssistantMessage constructs the assistant sdk.Message for a finalized
// stream turn. Reasoning is preserved whether or not tool calls are present -
// thinking-mode providers (e.g. Deepseek) reject follow-up requests with HTTP
// 400 if a prior assistant turn that produced reasoning is replayed without
// reasoning_content.
func buildAssistantMessage(
	content sdk.MessageContent,
	reasoning string,
	toolCalls []*sdk.ChatCompletionMessageToolCall,
) sdk.Message {
	msg := sdk.Message{
		Role:    sdk.Assistant,
		Content: content,
	}

	if reasoning != "" {
		r := reasoning
		msg.Reasoning = &r
		msg.ReasoningContent = &r
	}

	if len(toolCalls) > 0 {
		assistantToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			assistantToolCalls = append(assistantToolCalls, *tc)
		}
		msg.ToolCalls = &assistantToolCalls
	}

	return msg
}

// finalizeStream processes the completed stream and transitions to next state
func (a *EventDrivenAgent) finalizeStream(
	message sdk.Message,
	allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk,
	streamUsage *sdk.CompletionUsage,
	iterationStartTime time.Time,
) {
	a.service.accumulateToolCalls(allToolCallDeltas)
	toolCalls := a.service.getAccumulatedToolCalls()

	assistantContent := message.Content
	if _, err := assistantContent.AsMessageContent0(); err != nil {
		assistantContent = sdk.NewMessageContent("")
	}

	reasoning := ""
	if message.Reasoning != nil && *message.Reasoning != "" {
		reasoning = *message.Reasoning
	} else if message.ReasoningContent != nil && *message.ReasoningContent != "" {
		reasoning = *message.ReasoningContent
	}

	assistantMessage := buildAssistantMessage(assistantContent, reasoning, toolCalls)

	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, assistantMessage)

	assistantEntry := domain.ConversationEntry{
		Message:          assistantMessage,
		ReasoningContent: reasoning,
		Model:            a.req.Model,
		Time:             time.Now(),
	}

	if err := a.service.conversationRepo.AddMessage(assistantEntry); err != nil {
		logger.Error("failed to store assistant message", "error", err)
	}

	var completeToolCalls []sdk.ChatCompletionMessageToolCall
	if len(toolCalls) > 0 {
		completeToolCalls = make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			completeToolCalls = append(completeToolCalls, *tc)
		}
	}

	outputContent, _ := assistantContent.AsMessageContent0()
	polyfillInput := &storeIterationMetricsInput{
		inputMessages:   (*a.agentCtx.Conversation)[:len(*a.agentCtx.Conversation)-1],
		outputContent:   outputContent,
		outputToolCalls: completeToolCalls,
		availableTools:  a.availableTools,
	}

	a.service.storeIterationMetrics(a.req.RequestID, a.req.Model, iterationStartTime, streamUsage, polyfillInput)

	toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(completeToolCalls))
	for i := range completeToolCalls {
		toolCallsSlice = append(toolCallsSlice, &completeToolCalls[i])
	}

	a.events <- domain.StreamCompletedEvent{
		Message:            assistantMessage,
		ToolCalls:          toolCallsSlice,
		Reasoning:          reasoning,
		Usage:              streamUsage,
		IterationStartTime: iterationStartTime,
	}
}
