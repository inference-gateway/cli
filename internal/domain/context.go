package domain

// ContextKey is the type used for context keys in the application
type ContextKey string

// ToolApprovedKey is the context key for user-approved tool executions
// When this key is set to true in the context, it indicates that the tool
// execution was explicitly approved by the user and should bypass whitelist validation
const ToolApprovedKey ContextKey = "tool_approved"
