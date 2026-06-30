package domain

// UINotifier delivers a background-originated event to the single Bubble Tea
// Update loop. It is the one ingress every background producer uses to push work
// or status changes into the UI, replacing the per-source self-rescheduling
// pollers. The only production implementation wraps (*tea.Program).Send and lives
// in cmd/chat.go; keeping this interface tea-free lets services depend on it
// without importing bubbletea. The event is an `any` (tea.Msg is itself `any`).
type UINotifier interface {
	Notify(event any)
}

// NoopUINotifier is the useful zero value: producers can always call Notify even
// before the program exists or after shutdown, with no nil checks. The container
// defaults to it until cmd/chat.go swaps in the real (program-backed) notifier.
type NoopUINotifier struct{}

// Notify discards the event.
func (NoopUINotifier) Notify(any) {}

// NotifierFunc adapts a plain function to UINotifier. Tests use it to record the
// events a producer pushes without a generated mock.
type NotifierFunc func(event any)

// Notify forwards to the wrapped function when non-nil.
func (f NotifierFunc) Notify(event any) {
	if f != nil {
		f(event)
	}
}
