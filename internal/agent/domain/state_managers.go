// Session state the agent core reads and writes (agent mode, todos,
// computer-use pause, retry status). statemanager.StateManager implements
// them; the TUI-only state contracts live in internal/presentation/tui.

package domain

import ()

// AgentModeManager handles agent mode switching
type AgentModeManager interface {
	GetAgentMode() AgentMode
	SetAgentMode(mode AgentMode)
	CycleAgentMode() AgentMode
}

// RetryStatusSink receives the agent's retry progress so a UI can show it.
type RetryStatusSink interface {
	SetRetryStatus(status *RetryStatus)
}

// TodoManager handles todo list state
type TodoManager interface {
	SetTodos(todos []TodoItem)
	GetTodos() []TodoItem
}

// ComputerUsePauseManager handles computer use pause state
type ComputerUsePauseManager interface {
	SetComputerUsePaused(paused bool, requestID string)
	IsComputerUsePaused() bool
	GetPausedRequestID() string
	ClearComputerUsePauseState()
}
