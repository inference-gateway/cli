package domain

// ContextKey is the type used for context keys in the application
type ContextKey string

// ToolApprovedKey is the context key for user-approved tool executions
// When this key is set to true in the context, it indicates that the tool
// execution was explicitly approved by the user and should bypass allowed list validation
const ToolApprovedKey ContextKey = "tool_approved"

// BashOutputCallbackKey is the context key for bash output streaming callback
// When this key is set in the context, the bash tool streams output to the
// callback as it runs instead of waiting for the command to complete
const BashOutputCallbackKey ContextKey = "bash_output_callback"

// BashOutputCallback receives streaming bash output. Output is coalesced before
// delivery, so a single invocation may carry several newline-joined lines (the
// argument never has a trailing newline). This keeps the number of callbacks
// bounded for high-volume commands; the full command output is captured
// separately by the tool and is unaffected.
type BashOutputCallback func(output string)

// BashDetachChannelKey is the context key for the bash detach signal channel
// When this key is set in the context, the bash tool can signal when a command
// should be detached to the background (e.g., via keyboard shortcut)
const BashDetachChannelKey ContextKey = "bash_detach_channel"

// ChatHandlerKey is the context key for passing the ChatHandler reference
// This allows the agent service to access ChatHandler for setting up the detach channel
const ChatHandlerKey ContextKey = "chat_handler"

// SessionIDKey is the context key for the current conversation session ID
// This allows shortcuts to access the session ID when they need it (e.g., /export)
const SessionIDKey ContextKey = "session_id"

// DirectExecutionKey is the context key for direct tool execution
// When this key is set to true in the context, it indicates that the tool
// was invoked directly by the user (e.g., via !! command) rather than by the LLM
// This allows tools to adjust behavior (e.g., skip coordinate scaling for mouse operations)
const DirectExecutionKey ContextKey = "direct_execution"

// AgentModeKey is the context key for the agent mode in effect for a tool
// execution. The Bash tool reads it to resolve which per-mode allow-list
// (tools.bash.mode.<key>.allow) governs the command. When unset, callers treat
// it as standard mode.
const AgentModeKey ContextKey = "agent_mode"

// ModelKey is the context key for the model in effect for the current agent
// turn. The Agent tool reads it so spawned subagents inherit the parent's model
// by default (otherwise the subagent process would fail with "no model specified").
const ModelKey ContextKey = "model"

// ToolCallIDKey is the context key for the LLM tool call id of the current tool execution
const ToolCallIDKey ContextKey = "tool_call_id"

// TraceEnvKey is the context key for the W3C trace-context subprocess environment
const TraceEnvKey ContextKey = "trace_env"

// UserQuestionBrokerKey is the context key for the interactive question broker.
// It is injected only on the chat path (where a TUI event loop exists), so the
// AskUserQuestion tool sees a nil broker on headless/no-TTY runs and degrades
// gracefully instead of blocking forever.
const UserQuestionBrokerKey ContextKey = "user_question_broker"
