package domain

import "time"

// ToolInfo represents basic tool information for UI display
type ToolInfo struct {
	CallID string
	Name   string
	Status string
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
	Status     string
	Message    string
}

// ParallelToolsCompleteEvent indicates all parallel tools have completed
type ParallelToolsCompleteEvent struct {
	BaseChatEvent
	TotalExecuted int
	SuccessCount  int
	FailureCount  int
	Duration      time.Duration
}
