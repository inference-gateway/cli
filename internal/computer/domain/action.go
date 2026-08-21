// Package domain holds the computer-use bounded context's contracts:
// actions the agent can perform, targets they act on, observations they
// return, and the capabilities the platform supports. It is pure - stdlib
// imports only.
package domain

// ActionKind enumerates what the Computer tool can do.
type ActionKind string

const (
	ActionScreenshot  ActionKind = "screenshot"
	ActionCursor      ActionKind = "cursor"
	ActionMove        ActionKind = "move"
	ActionClick       ActionKind = "click"
	ActionDoubleClick ActionKind = "double_click"
	ActionTripleClick ActionKind = "triple_click"
	ActionScroll      ActionKind = "scroll"
	ActionType        ActionKind = "type"
	ActionKey         ActionKind = "key"
)

// Action is one computer-use request: what to do and, where relevant, on
// what target and with what input.
type Action struct {
	Kind      ActionKind
	Target    *Target
	Text      string // type: the text to type
	Combo     string // key: a combo such as "cmd+shift+t"
	Button    string // click kinds: left (default), right, middle
	Direction string // scroll: vertical (default) or horizontal
	Amount    int    // scroll: wheel clicks; negative reverses direction
}
