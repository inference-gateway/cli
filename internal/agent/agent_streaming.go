package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	sdk "github.com/inference-gateway/sdk"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// errConnectStalled marks a stream request that produced no response within
// the stall threshold, so the reconnect loop retries it instead of failing.
var errConnectStalled = errors.New("stream connect stalled")

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

	maxReconnects := 0
	if retryCfg := a.service.config.Client.Retry; retryCfg.Enabled {
		maxReconnects = retryCfg.MaxAttempts
	}

	for attempt := 0; ; attempt++ {
		if !a.streamOnce(client, iterationStartTime) {
			return
		}

		if attempt >= maxReconnects {
			a.failStream(fmt.Errorf("connection lost: stream stalled after %d reconnect attempts", maxReconnects))
			return
		}

		if a.service.stateManager != nil {
			a.service.stateManager.SetRetryStatus(&domain.RetryStatus{Attempt: attempt + 1, MaxAttempts: maxReconnects})
		}
		a.eventPublisher.publishChatStart()

		select {
		case <-a.agentCtx.Ctx.Done():
			return
		case <-time.After(a.reconnectBackoff(attempt)):
		}
	}
}

// streamOnce runs a single streaming request end to end. It returns true when
// the stream broke mid-flight (stalled or transport error) and the caller
// should reconnect, false when the turn finished - successfully, cancelled, or
// with a non-recoverable error that has already been published.
func (a *EventDrivenAgent) streamOnce(client sdk.Client, iterationStartTime time.Time) bool {
	requestCtx, requestCancel := context.WithTimeout(a.agentCtx.Ctx, time.Duration(a.service.timeoutSeconds)*time.Second)
	defer requestCancel()

	requestCtx, turnSpan := a.service.recorder.StartLLMTurnSpan(requestCtx, a.req.Model)
	defer turnSpan.End()

	events, err := a.openStream(requestCtx, requestCancel, client)
	if err != nil {
		if errors.Is(err, errConnectStalled) {
			logger.Warn("stream connect stalled, reconnecting",
				"request_id", a.req.RequestID,
				"turn", a.agentCtx.Turns)
			return true
		}
		logger.Error("failed to create stream",
			"error", err,
			"turn", a.agentCtx.Turns,
			"conversationLength", len(*a.agentCtx.Conversation),
			"provider", a.provider)
		a.failStream(err)
		return false
	}

	broken := a.processStreamEvents(requestCtx, events, iterationStartTime)
	return broken
}

// openStream issues the streaming request bounded by the stall threshold: a
// request that produces no response within it (e.g. a TCP connect hanging on
// a dead network for the ~75s OS timeout) is cancelled and reported as
// errConnectStalled so the reconnect loop counts it like any other stall.
func (a *EventDrivenAgent) openStream(requestCtx context.Context, cancel context.CancelFunc, client sdk.Client) (<-chan sdk.SSEvent, error) {
	stallAfter := time.Duration(a.service.config.Client.StallThresholdSec) * time.Second
	if stallAfter <= 0 {
		return client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, *a.agentCtx.Conversation)
	}

	type opened struct {
		events <-chan sdk.SSEvent
		err    error
	}
	done := make(chan opened, 1)
	go func() {
		events, err := client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, *a.agentCtx.Conversation)
		done <- opened{events, err}
	}()

	timer := time.NewTimer(stallAfter)
	defer timer.Stop()
	select {
	case o := <-done:
		return o.events, o.err
	case <-timer.C:
		cancel()
		<-done
		return nil, errConnectStalled
	}
}

// failStream publishes a terminal stream error and moves the state machine to
// StateError.
func (a *EventDrivenAgent) failStream(err error) {
	a.eventPublisher.chatEvents <- domain.ChatErrorEvent{
		RequestID: a.req.RequestID,
		Timestamp: time.Now(),
		Error:     err,
	}
	if terr := a.stateMachine.Transition(a.agentCtx, domain.StateError); terr != nil {
		logger.Error("failed to transition to Error state after stream failure", "error", terr)
	}
	a.events <- domain.MessageReceivedEvent{}
}

// reconnectBackoff returns the exponential backoff delay before reconnect
// attempt number attempt+1, derived from the client retry config.
func (a *EventDrivenAgent) reconnectBackoff(attempt int) time.Duration {
	retryCfg := a.service.config.Client.Retry
	delay := time.Duration(retryCfg.InitialBackoffSec) * time.Second
	if delay <= 0 {
		delay = time.Second
	}
	for range attempt {
		delay = time.Duration(float64(delay) * float64(retryCfg.BackoffMultiplier))
	}
	if maxDelay := time.Duration(retryCfg.MaxBackoffSec) * time.Second; maxDelay > 0 && delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// processStreamEvents processes streaming events from the LLM. It returns true
// when the stream broke mid-flight - no events for the configured stall
// threshold, or a transport read error - so the caller can reconnect.
func (a *EventDrivenAgent) processStreamEvents(
	requestCtx context.Context,
	events <-chan sdk.SSEvent,
	iterationStartTime time.Time,
) bool {
	var allToolCallDeltas []sdk.ChatCompletionMessageToolCallChunk
	var message sdk.Message
	var streamUsage *sdk.CompletionUsage

	var stallC <-chan time.Time
	var stallTimer *time.Timer
	stallAfter := time.Duration(a.service.config.Client.StallThresholdSec) * time.Second
	if stallAfter > 0 {
		stallTimer = time.NewTimer(stallAfter)
		defer stallTimer.Stop()
		stallC = stallTimer.C
	}

	for {
		select {
		case <-requestCtx.Done():
			a.handleStreamInterrupted(requestCtx, message)
			return false

		case <-stallC:
			logger.Warn("stream stalled, reconnecting",
				"request_id", a.req.RequestID,
				"stalled_for", stallAfter.String())
			return true

		case event, ok := <-events:
			if !ok {
				a.finalizeStream(message, allToolCallDeltas, streamUsage, iterationStartTime)
				return false
			}

			if stallTimer != nil {
				stallTimer.Reset(stallAfter)
			}

			usage, broken := a.processStreamEvent(event, &message, &allToolCallDeltas)
			if broken {
				return true
			}
			if usage != nil {
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
		a.events <- domain.MessageReceivedEvent{}
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

// processStreamEvent processes a single SSE event from the stream. The second
// return value is true when the event signals a broken transport - the SDK
// emits an event with a nil Event type and an error payload when the
// connection drops mid-stream.
func (a *EventDrivenAgent) processStreamEvent(
	event sdk.SSEvent,
	message *sdk.Message,
	allToolCallDeltas *[]sdk.ChatCompletionMessageToolCallChunk,
) (*sdk.CompletionUsage, bool) {
	if event.Event == nil {
		if event.Data != nil {
			logger.Error("stream transport error", "data", string(*event.Data))
			return nil, true
		}
		return nil, false
	}

	if event.Data == nil {
		return nil, false
	}

	switch string(*event.Event) {
	case "message_stop", "system_init", "hook_event", "tool_failure", "result_metadata":
		return nil, false
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		logger.Error("failed to unmarshal chat completion stream response",
			"error", err,
			"raw_data", string(*event.Data))
		return nil, false
	}

	var streamUsage *sdk.CompletionUsage
	if streamResponse.Usage != nil {
		streamUsage = streamResponse.Usage
	}

	for _, choice := range streamResponse.Choices {
		a.processChoiceDelta(choice, message, allToolCallDeltas)
	}

	return streamUsage, false
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
