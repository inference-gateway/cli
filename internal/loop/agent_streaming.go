package loop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	states "github.com/inference-gateway/cli/internal/loop/states"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
)

// startStreaming implements the LLM streaming logic for the EventDrivenAgent
func (a *EventDrivenAgent) startStreaming() {
	defer a.recoverPanic()
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

	if !a.req.IsChatMode && a.agentCtx.Turns > 1 {
		a.service.maybeRolloverSession(a.agentCtx, a.req)
	}

	a.eventPublisher.publishChatStart()

	if a.agentCtx.Turns == 1 {
		a.service.dispatchHooks(a.agentCtx, agentdomain.HookPreSession)
	}
	a.service.dispatchHooks(a.agentCtx, agentdomain.HookPreStream)

	a.availableTools = a.service.advertisedTools()

	client := a.service.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens:       &a.service.maxTokens,
			ReasoningEffort: a.service.reasoningEffortOptionFor(a.req.Model),
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
			a.service.stateManager.SetRetryStatus(&agentdomain.RetryStatus{Attempt: attempt + 1, MaxAttempts: maxReconnects})
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
// the open stream broke mid-flight (no chunk within the stall threshold or a
// transport error) so the caller reconnects. Opening the stream is not
// retried here: the SDK client retries connection errors itself and reports
// the real cause, which ends the turn.
func (a *EventDrivenAgent) streamOnce(client sdk.Client, iterationStartTime time.Time) bool {
	a.finishReason = ""
	requestCtx, gotChunk, requestCancel := withFirstChunkDeadline(a.agentCtx.Ctx, time.Duration(a.service.timeoutSeconds)*time.Second)
	defer requestCancel()

	requestCtx, turnSpan := a.service.recorder.StartLLMTurnSpan(requestCtx, a.req.Model)
	defer turnSpan.End()

	events, err := client.GenerateContentStream(requestCtx, sdk.Provider(a.provider), a.model, a.outboundConversation())
	if err != nil {
		var rateLimit *sdk.RateLimitError
		if errors.As(err, &rateLimit) {
			logger.Warn("provider quota reached", "provider", a.provider, "retry_after", rateLimit.RetryAfter)
			a.failStream(rateLimitMessage(a.provider, rateLimit, time.Now()))
			return false
		}
		if errors.Is(context.Cause(requestCtx), context.DeadlineExceeded) {
			err = a.firstChunkTimeout()
		}
		logger.Error("failed to create stream",
			"error", err,
			"turn", a.agentCtx.Turns,
			"conversationLength", len(*a.agentCtx.Conversation),
			"provider", a.provider)
		telemetry.SetSpanError(requestCtx, err)
		a.failStream(err)
		return false
	}

	broken := a.processStreamEvents(requestCtx, events, iterationStartTime, gotChunk)
	return broken
}

// firstChunkTimeout names the setting to raise when no chunk arrived in time.
func (a *EventDrivenAgent) firstChunkTimeout() error {
	return fmt.Errorf("no response within %d seconds (gateway.timeout)", a.service.timeoutSeconds)
}

// withFirstChunkDeadline cancels ctx with context.DeadlineExceeded unless
// gotChunk is called within timeout, so the timeout bounds the wait for a
// response, never its length. The stall threshold guards it from then on.
func withFirstChunkDeadline(parent context.Context, timeout time.Duration) (ctx context.Context, gotChunk, cancel func()) {
	ctx, cancelCause := context.WithCancelCause(parent)
	deadline := time.AfterFunc(timeout, func() { cancelCause(context.DeadlineExceeded) })
	return ctx, func() { deadline.Stop() }, func() {
		deadline.Stop()
		cancelCause(nil)
	}
}

// outboundConversation returns the request payload: the shared conversation
// plus the ephemeral volatile-context tail, rebuilt here per request so the
// volatile sections (git branch, tree, memory, active skill, date) stay fresh
// across a long agent loop instead of freezing at RunWithStream time. The
// shared slice is cloned before appending so the tail never leaks into
// persistence, the TUI, or later turns. The tail decision lives here — per
// request, after ensureConversationIntegrity has repaired the conversation —
// and is skipped while an assistant tool_call is still unanswered, where a
// trailing user message would orphan it. Rebuilding is prompt-cache-safe: the
// tail always trails the newest messages, so it is never part of a reusable
// token prefix.
func (a *EventDrivenAgent) outboundConversation() []sdk.Message {
	conversation := *a.agentCtx.Conversation
	if conversationAwaitsToolResults(conversation) {
		return conversation
	}
	if tail, ok := a.service.volatileTailMessage(conversation, a.req.IsChatMode); ok {
		conversation = append(slices.Clone(conversation), tail)
	}
	return conversation
}

// recoverPanic converts a panic on an agent goroutine into the terminal
// stream-error path (ChatErrorEvent + StateError) so headless consumers get an
// agent_error line instead of a process crash. Deferred at every goroutine root.
func (a *EventDrivenAgent) recoverPanic() {
	if r := recover(); r != nil {
		logger.Error("agent panic recovered", "panic", r, "stack", string(debug.Stack()))
		a.failStream(fmt.Errorf("agent panic: %v", r))
	}
}

// rateLimitMessage turns a provider quota wall into the one line the user
// needs: what the provider said and when they can continue.
func rateLimitMessage(provider string, err *sdk.RateLimitError, now time.Time) error {
	resume := now.Add(err.RetryAfter)
	wait := err.RetryAfter.Round(time.Minute)
	return fmt.Errorf("%s usage limit reached: %s. You can continue at %s (in %dh %02dm)",
		provider, err.Message, resume.Format("15:04"), int(wait.Hours()), int(wait.Minutes())%60)
}

// failStream publishes a terminal stream error and moves the state machine to
// StateError.
func (a *EventDrivenAgent) failStream(err error) {
	a.eventPublisher.chatEvents <- agentdomain.ChatErrorEvent{
		RequestID: a.req.RequestID,
		Timestamp: time.Now(),
		Error:     err,
	}
	if terr := a.stateMachine.Transition(a.agentCtx, states.StateError); terr != nil {
		logger.Error("failed to transition to Error state after stream failure", "error", terr)
	}
	a.events <- states.MessageReceivedEvent{}
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
	gotChunk func(),
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
				a.finalizeStream(requestCtx, message, allToolCallDeltas, streamUsage, iterationStartTime)
				return false
			}

			gotChunk()
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
	if cause := context.Cause(requestCtx); errors.Is(cause, context.DeadlineExceeded) {
		logger.Error("stream timeout", "error", cause)
		telemetry.SetSpanError(requestCtx, cause)
		a.eventPublisher.chatEvents <- agentdomain.ChatErrorEvent{
			RequestID: a.req.RequestID,
			Timestamp: time.Now(),
			Error:     a.firstChunkTimeout(),
		}
		if err := a.stateMachine.Transition(a.agentCtx, states.StateError); err != nil {
			logger.Error("failed to transition to Error state after stream failure", "error", err)
		}
		a.events <- states.MessageReceivedEvent{}
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

	entry := convdomain.ConversationEntry{
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
	if choice.FinishReason != "" {
		a.finishReason = string(choice.FinishReason)
	}

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
	ctx context.Context,
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

	inputMessages := a.outboundConversation()
	*a.agentCtx.Conversation = append(*a.agentCtx.Conversation, assistantMessage)

	assistantEntry := convdomain.ConversationEntry{
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
		inputMessages:   inputMessages,
		outputContent:   outputContent,
		outputToolCalls: completeToolCalls,
		availableTools:  a.availableTools,
	}

	a.service.storeIterationMetrics(ctx, a.req.RequestID, a.req.Model, iterationStartTime, streamUsage, polyfillInput)

	toolCallsSlice := make([]*sdk.ChatCompletionMessageToolCall, 0, len(completeToolCalls))
	for i := range completeToolCalls {
		toolCallsSlice = append(toolCallsSlice, &completeToolCalls[i])
	}

	a.service.trackStreamOutcome(a.finishReason, len(toolCallsSlice) > 0, strings.TrimSpace(outputContent) != "")

	a.events <- states.StreamCompletedEvent{
		Message:            assistantMessage,
		ToolCalls:          toolCallsSlice,
		Reasoning:          reasoning,
		Usage:              streamUsage,
		IterationStartTime: iterationStartTime,
	}
}
