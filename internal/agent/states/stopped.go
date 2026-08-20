package states

// StoppedState handles events in the Stopped state.
//
// The Stopped state is a terminal state reached when the agent stops execution
// (e.g., due to tool rejection or other stop conditions).
// The event loop will exit when this state is reached.
type StoppedState struct {
	ctx *StateContext
}

// NewStoppedState creates a new Stopped state handler
func NewStoppedState(ctx *StateContext) StateHandler {
	return &StoppedState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *StoppedState) Name() AgentExecutionState {
	return StateStopped
}

// Handle processes events in Stopped state
// This is a terminal state, so no events are expected
func (s *StoppedState) Handle(event AgentEvent) error {
	// Terminal state - no events to handle
	return nil
}
