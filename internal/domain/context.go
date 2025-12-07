package domain

// ContextKey is the type used for context keys in the application
type ContextKey string

// ToolApprovedKey is the context key for user-approved tool executions
// When this key is set to true in the context, it indicates that the tool
// execution was explicitly approved by the user and should bypass whitelist validation
const ToolApprovedKey ContextKey = "tool_approved"

// BashOutputCallbackKey is the context key for bash output streaming callback
// When this key is set in the context, the bash tool will stream output line by line
// through the callback function instead of waiting for the command to complete
const BashOutputCallbackKey ContextKey = "bash_output_callback"

// BashOutputCallback is a function type for receiving streaming bash output
type BashOutputCallback func(line string)

// BashDetachChannelKey is the context key for the bash detach signal channel
// When this key is set in the context, the bash tool can signal when a command
// should be detached to the background (e.g., via keyboard shortcut)
const BashDetachChannelKey ContextKey = "bash_detach_channel"

// ChatHandlerKey is the context key for passing the ChatHandler reference
// This allows the agent service to access ChatHandler for setting up the detach channel
const ChatHandlerKey ContextKey = "chat_handler"
