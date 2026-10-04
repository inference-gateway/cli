package states

// InputRequiredState handles events in the InputRequired state.
//
// The run is blocked on the user and resumes into the state it left once the
// prompt is answered. No loop event is emitted while a prompt is open.
type InputRequiredState struct {
	ctx *StateContext
}

// NewInputRequiredState creates a new InputRequired state handler
func NewInputRequiredState(ctx *StateContext) StateHandler {
	return &InputRequiredState{ctx: ctx}
}

// Name returns the state this handler manages
func (s *InputRequiredState) Name() AgentExecutionState {
	return StateInputRequired
}

// Handle processes events in InputRequired state
func (s *InputRequiredState) Handle(event AgentEvent) error {
	return nil
}
