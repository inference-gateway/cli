package domain

import "time"

// ToolInfo represents basic tool information for UI display
type ToolInfo struct {
	CallID    string
	Name      string
	Status    string
	Arguments string
}

// BaseChatEvent provides common implementation for ChatEvent interface
type BaseChatEvent struct {
	RequestID string
	Timestamp time.Time
}

func (e BaseChatEvent) GetRequestID() string    { return e.RequestID }
func (e BaseChatEvent) GetTimestamp() time.Time { return e.Timestamp }

// ParallelToolsStartEvent indicates parallel tool execution has started
type ParallelToolsStartEvent struct {
	BaseChatEvent
	Tools []ToolInfo
}

// ToolExecutionProgressEvent indicates progress in tool execution
type ToolExecutionProgressEvent struct {
	BaseChatEvent
	ToolCallID string
	ToolName   string
	Status     string
	Message    string
}

// BashOutputChunkEvent indicates a new chunk of bash output is available
type BashOutputChunkEvent struct {
	BaseChatEvent
	ToolCallID string
	Output     string
	IsComplete bool
}

// ParallelToolsCompleteEvent indicates all parallel tools have completed
type ParallelToolsCompleteEvent struct {
	BaseChatEvent
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Duration      time.Duration
}

// TodoUpdateChatEvent indicates the todo list has been updated (flows through chat event channel)
type TodoUpdateChatEvent struct {
	BaseChatEvent
	Todos []TodoItem
}

// BorderOverlayEvent indicates the screen border overlay should be shown or hidden
type BorderOverlayEvent struct {
	BaseChatEvent
	BorderAction string
}

// ClickIndicatorEvent indicates a visual click indicator should be shown at coordinates
type ClickIndicatorEvent struct {
	BaseChatEvent
	X              int  `json:"X"`
	Y              int  `json:"Y"`
	ClickIndicator bool `json:"ClickIndicator"`
}

// MoveIndicatorEvent indicates a visual move indicator should be shown at coordinates
type MoveIndicatorEvent struct {
	BaseChatEvent
	FromX         int  `json:"FromX"`
	FromY         int  `json:"FromY"`
	ToX           int  `json:"ToX"`
	ToY           int  `json:"ToY"`
	MoveIndicator bool `json:"MoveIndicator"`
}
