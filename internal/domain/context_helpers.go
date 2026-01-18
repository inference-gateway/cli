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
