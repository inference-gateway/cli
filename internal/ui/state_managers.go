// View transitions are presentation state, so ViewManager lives here; the
// other state accessor contracts (chat session, tool execution, approvals,
// todos, computer-use pause) live in agent/domain as the shared kernel.

package ui

// ViewManager handles view state transitions
type ViewManager interface {
	GetCurrentView() ViewState
	GetPreviousView() ViewState
	TransitionToView(newView ViewState) error
}
