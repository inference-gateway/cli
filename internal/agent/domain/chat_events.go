package domain

import "time"

// All events in this file implement tea.Msg (Bubble Tea's message interface) and are part
// of the Bubble Tea message system. These events represent chat-specific operations like
// tool execution and progress tracking.

// BaseChatEvent provides common implementation for ChatEvent interface
type BaseChatEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e BaseChatEvent) GetRequestID() string    { return e.RequestID }
func (e BaseChatEvent) GetTimestamp() time.Time { return e.Timestamp }

// UserMessageChatEvent mirrors a user message that entered the conversation
// (typed in the TUI, queued, or sent from the extension) so external consumers
// such as the extension bridge can render it. Content is the plain text.
type UserMessageChatEvent struct {
	BaseChatEvent
	Content string
}

// ToolExecutionProgressEvent indicates progress in tool execution
type ToolExecutionProgressEvent struct {
	BaseChatEvent
	ToolCallID string
	ToolName   string
	Arguments  string
	Status     string
	Message    string
	Images     []ImageAttachment
}

// BashOutputChunkEvent indicates a new chunk of bash output is available
type BashOutputChunkEvent struct {
	BaseChatEvent
	ToolCallID string
	Output     string
	IsComplete bool
}

// TodoUpdateChatEvent indicates the todo list has been updated (flows through chat event channel)
type TodoUpdateChatEvent struct {
	BaseChatEvent
	Todos []TodoItem
}
