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

	if a.service.shouldInjectSystemReminder(a.agentCtx.Turns) {
		systemReminderMsg := a.service.getSystemReminderMessage()
		*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, systemReminderMsg)

		reminderEntry := domain.ConversationEntry{
			Message: systemReminderMsg,
			Time:    time.Now(),
			Hidden:  true,
		}

		if err := a.service.conversationRepo.AddMessage(reminderEntry); err != nil {
			logger.Error("failed to store system reminder message", "error", err)
		}
	}

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

	events, err := client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, *a.agentCtx.Conversation)
	if err != nil {
		logger.Error("Failed to create stream",
			"error", err,
			"turn", a.agentCtx.Turns,
			"conversationLength", len(*a.agentCtx.Conversation),
			"provider", a.provider)
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     err,
		}
		_ = a.stateMachine.Transition(a.agentCtx, domain.StateError)
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
			a.handleContextTimeout(requestCtx)
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

// handleContextTimeout handles context cancellation and timeout errors
func (a *EventDrivenAgent) handleContextTimeout(requestCtx context.Context) {
	if requestCtx.Err() == context.DeadlineExceeded {
		logger.Error("stream timeout", "error", requestCtx.Err())
		a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     fmt.Errorf("stream timed out after %d seconds", a.service.timeoutSeconds),
		}
	}
	_ = a.stateMachine.Transition(a.agentCtx, domain.StateError)
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

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		logger.Error("failed to unmarshal chat completion stream response")
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

	if len(choice.Delta.ToolCalls) > 0 {
		*allToolCallDeltas = append(*allToolCallDeltas, choice.Delta.ToolCalls...)
	}

	if deltaContent != "" || reasoning != "" || len(choice.Delta.ToolCalls) > 0 {
		a.eventPublisher.publishChatChunk(deltaContent, reasoning, choice.Delta.ToolCalls)
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

	assistantMessage := sdk.Message{
		Role:    sdk.Assistant,
		Content: assistantContent,
	}

	if len(toolCalls) > 0 {
		assistantToolCalls := make([]sdk.ChatCompletionMessageToolCall, 0, len(toolCalls))
		for _, tc := range toolCalls {
			assistantToolCalls = append(assistantToolCalls, *tc)
		}
		assistantMessage.ToolCalls = &assistantToolCalls

		if reasoning != "" {
			assistantMessage.Reasoning = &reasoning
			assistantMessage.ReasoningContent = &reasoning
		}
	}

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
