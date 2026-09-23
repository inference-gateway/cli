package domain

// contextKey is the type used for context keys in the application
type contextKey string

// toolApprovedKey is the context key for user-approved tool executions
// When this key is set to true in the context, it indicates that the tool
// execution was explicitly approved by the user and should bypass allowed list validation
const toolApprovedKey contextKey = "tool_approved"

// bashOutputCallbackKey is the context key for bash output streaming callback
// When this key is set in the context, the bash tool streams output to the
// callback as it runs instead of waiting for the command to complete
const bashOutputCallbackKey contextKey = "bash_output_callback"

// BashOutputCallback receives streaming bash output. Output is coalesced before
// delivery, so a single invocation may carry several newline-joined lines (the
// argument never has a trailing newline). This keeps the number of callbacks
// bounded for high-volume commands; the full command output is captured
// separately by the tool and is unaffected.
type BashOutputCallback func(output string)

// toolProgressCallbackKey is the context key for a tool progress callback.
const toolProgressCallbackKey contextKey = "tool_progress_callback"

// ToolProgressCallback receives a progress line describing what a tool is
// currently doing. Callers throttle their own reporting; a callback must be
// safe to invoke from the goroutine running the tool.
type ToolProgressCallback func(message string)

// bashDetachChannelKey is the context key for the bash detach signal channel
// When this key is set in the context, the bash tool can signal when a command
// should be detached to the background (e.g., via keyboard shortcut)
const bashDetachChannelKey contextKey = "bash_detach_channel"

// chatHandlerKey is the context key for passing the ChatHandler reference
// This allows the agent service to access ChatHandler for setting up the detach channel
const chatHandlerKey contextKey = "chat_handler"

// sessionIDKey is the context key for the current conversation session ID
// This allows shortcuts to access the session ID when they need it (e.g., /export)
const sessionIDKey contextKey = "session_id"

// directExecutionKey is the context key for direct tool execution
// When this key is set to true in the context, it indicates that the tool
// was invoked directly by the user (e.g., via !! command) rather than by the LLM
// This allows tools to adjust behavior (e.g., skip coordinate scaling for mouse operations)
const directExecutionKey contextKey = "direct_execution"

// agentModeKey is the context key for the agent mode in effect for a tool
// execution. The Bash tool reads it to resolve which per-mode allow-list
// (tools.bash.mode.<key>.allow) governs the command. When unset, callers treat
// it as standard mode.
const agentModeKey contextKey = "agent_mode"

// modelKey is the context key for the model in effect for the current agent
// turn. The Agent tool reads it so spawned subagents inherit the parent's model
// by default (otherwise the subagent process would fail with "no model specified").
const modelKey contextKey = "model"

// toolCallIDKey is the context key for the LLM tool call id of the current tool execution
const toolCallIDKey contextKey = "tool_call_id"

// traceEnvKey is the context key for the W3C trace-context subprocess environment
const traceEnvKey contextKey = "trace_env"

// sandboxApprovalKey is the context key marking that a user can answer a
// sandbox-extension prompt in this run (chat TUI, or headless with an IPC
// approval broker attached). When unset, sandbox denials fail as before.
const sandboxApprovalKey contextKey = "sandbox_approval"

// userQuestionsAvailableKey marks a run whose host can answer AskUserQuestion forms.
const userQuestionsAvailableKey contextKey = "user_questions_available"

// userQuestionBrokerKey is the context key for the interactive question broker.
// It is injected only on the chat path (where a TUI event loop exists), so the
// AskUserQuestion tool sees a nil broker on headless/no-TTY runs and degrades
// gracefully instead of blocking forever.
const userQuestionBrokerKey contextKey = "user_question_broker"

// approvalEscalationKey is the context key for the judge-rejection escalation
// gate used by the RequestApproval tool. Injected only on the chat path, where
// the tool approval box can reach the user; headless/no-TTY runs see a
// nil gate and the tool returns a distinguishable "no approver reachable" result.
const approvalEscalationKey contextKey = "approval_escalation"
