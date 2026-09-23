// Session state the agent core reads and writes (agent mode, todos,
// computer-use pause, retry status). statemanager.Store implements
// them; the TUI-only state contracts live in internal/presentation/tui.

package domain

import ()

// AgentModeState handles agent mode switching
type AgentModeState interface {
	GetAgentMode() AgentMode
	SetAgentMode(mode AgentMode)
	CycleAgentMode() AgentMode
}

// RetryStatusSink receives the agent's retry progress so a UI can show it.
type RetryStatusSink interface {
	SetRetryStatus(status *RetryStatus)
}

// TodoList handles todo list state
type TodoList interface {
	SetTodos(todos []TodoItem)
	GetTodos() []TodoItem
}

// ComputerUsePause handles computer use pause state
type ComputerUsePause interface {
	SetComputerUsePaused(paused bool, requestID string)
	IsComputerUsePaused() bool
	GetPausedRequestID() string
	ClearComputerUsePauseState()
}
