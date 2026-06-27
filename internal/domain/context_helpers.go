package domain

import "context"

// Type-safe context helper functions
// These functions provide type-safe ways to set and retrieve context values,
// eliminating the need for type assertions throughout the codebase.

// ========================================
// Tool Approval
// ========================================

// WithToolApproved returns a new context with ToolApprovedKey set to true
func WithToolApproved(ctx context.Context) context.Context {
	return context.WithValue(ctx, ToolApprovedKey, true)
}

// IsToolApproved checks if the tool was explicitly approved by the user
// Returns false if the key is not set or if the value is not a bool
func IsToolApproved(ctx context.Context) bool {
	val, ok := ctx.Value(ToolApprovedKey).(bool)
	return ok && val
}

// ========================================
// Direct Execution
// ========================================

// WithDirectExecution returns a new context with DirectExecutionKey set to true
func WithDirectExecution(ctx context.Context) context.Context {
	return context.WithValue(ctx, DirectExecutionKey, true)
}

// IsDirectExecution checks if the tool was invoked directly by the user
// Returns false if the key is not set or if the value is not a bool
func IsDirectExecution(ctx context.Context) bool {
	val, ok := ctx.Value(DirectExecutionKey).(bool)
	return ok && val
}

// ========================================
// Agent Mode
// ========================================

// WithAgentMode returns a new context carrying the agent mode in effect for a
// tool execution. The Bash tool reads it (via BashAllowModeKey) to pick the
// per-mode allow-list that governs the command.
func WithAgentMode(ctx context.Context, mode AgentMode) context.Context {
	return context.WithValue(ctx, AgentModeKey, mode)
}

// AgentModeFromContext returns the agent mode stored in ctx and whether it was
// present. When absent, callers should default to AgentModeStandard.
func AgentModeFromContext(ctx context.Context) (AgentMode, bool) {
	mode, ok := ctx.Value(AgentModeKey).(AgentMode)
	return mode, ok
}

// BashAllowModeKey returns the bash allow-list mode key for the agent mode in
// ctx, defaulting to "standard" when no mode is set. Convenience for the Bash
// tool and the approval policy so they resolve the same per-mode allow-list.
func BashAllowModeKey(ctx context.Context) string {
	if mode, ok := AgentModeFromContext(ctx); ok {
		return mode.AllowedlistKey()
	}
	return "standard"
}

// ========================================
// Bash Output Callback
// ========================================

// WithBashOutputCallback returns a new context with a bash output streaming callback
func WithBashOutputCallback(ctx context.Context, callback BashOutputCallback) context.Context {
	return context.WithValue(ctx, BashOutputCallbackKey, callback)
}

// GetBashOutputCallback retrieves the bash output callback from context
// Returns nil if the key is not set or if the value is not a BashOutputCallback
func GetBashOutputCallback(ctx context.Context) BashOutputCallback {
	callback, _ := ctx.Value(BashOutputCallbackKey).(BashOutputCallback)
	return callback
}

// HasBashOutputCallback checks if a bash output callback is set in the context
func HasBashOutputCallback(ctx context.Context) bool {
	return GetBashOutputCallback(ctx) != nil
}

// ========================================
// Bash Detach Channel
// ========================================

// WithBashDetachChannel returns a new context with a bash detach signal channel
func WithBashDetachChannel(ctx context.Context, ch <-chan struct{}) context.Context {
	return context.WithValue(ctx, BashDetachChannelKey, ch)
}

// GetBashDetachChannel retrieves the bash detach channel from context
// Returns nil if the key is not set or if the value is not a channel
func GetBashDetachChannel(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(BashDetachChannelKey).(<-chan struct{})
	return ch
}

// HasBashDetachChannel checks if a bash detach channel is set in the context
func HasBashDetachChannel(ctx context.Context) bool {
	return GetBashDetachChannel(ctx) != nil
}

// ========================================
// Chat Handler
// ========================================

// WithChatHandler returns a new context with a ChatHandler reference
func WithChatHandler(ctx context.Context, handler BashDetachChannelHolder) context.Context {
	return context.WithValue(ctx, ChatHandlerKey, handler)
}

// GetChatHandler retrieves the ChatHandler from context
// Returns nil if the key is not set or if the value is not a BashDetachChannelHolder
func GetChatHandler(ctx context.Context) BashDetachChannelHolder {
	handler, _ := ctx.Value(ChatHandlerKey).(BashDetachChannelHolder)
	return handler
}

// HasChatHandler checks if a ChatHandler is set in the context
func HasChatHandler(ctx context.Context) bool {
	return GetChatHandler(ctx) != nil
}

// ========================================
// Session ID
// ========================================

// WithSessionID returns a new context with a session ID
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionIDKey, sessionID)
}

// WithModel returns a new context carrying the model in effect for the current
// agent turn. The Agent tool reads it so subagents inherit the parent's model.
func WithModel(ctx context.Context, model string) context.Context {
	return context.WithValue(ctx, ModelKey, model)
}

// GetModel retrieves the model from the context, or "" if not set.
func GetModel(ctx context.Context) string {
	model, _ := ctx.Value(ModelKey).(string)
	return model
}

// GetSessionID retrieves the session ID from context
// Returns empty string if the key is not set or if the value is not a string
func GetSessionID(ctx context.Context) string {
	sessionID, _ := ctx.Value(SessionIDKey).(string)
	return sessionID
}

// HasSessionID checks if a session ID is set in the context
func HasSessionID(ctx context.Context) bool {
	return GetSessionID(ctx) != ""
}

// ========================================
// User Question Broker
// ========================================

// WithUserQuestionBroker returns a new context carrying the interactive
// question broker used by the AskUserQuestion tool. Injected only on the chat
// path so headless/no-TTY runs see a nil broker and degrade gracefully.
func WithUserQuestionBroker(ctx context.Context, broker UserQuestionBroker) context.Context {
	return context.WithValue(ctx, UserQuestionBrokerKey, broker)
}

// GetUserQuestionBroker retrieves the question broker from context.
// Returns nil if the key is not set or the value is not a UserQuestionBroker.
func GetUserQuestionBroker(ctx context.Context) UserQuestionBroker {
	broker, _ := ctx.Value(UserQuestionBrokerKey).(UserQuestionBroker)
	return broker
}

// HasUserQuestionBroker checks if a question broker is set in the context.
func HasUserQuestionBroker(ctx context.Context) bool {
	return GetUserQuestionBroker(ctx) != nil
}
