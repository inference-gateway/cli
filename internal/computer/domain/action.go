package domain

// ActionKind enumerates what the Computer tool can do.
type ActionKind string

const (
	ActionScreenshot    ActionKind = "screenshot"
	ActionAccessibility ActionKind = "accessibility"
	ActionCursor        ActionKind = "cursor"
	ActionMove          ActionKind = "move"
	ActionClick         ActionKind = "click"
	ActionDoubleClick   ActionKind = "double_click"
	ActionTripleClick   ActionKind = "triple_click"
	ActionScroll        ActionKind = "scroll"
	ActionType          ActionKind = "type"
	ActionKey           ActionKind = "key"
	ActionPress         ActionKind = "press"
	ActionMouseDown     ActionKind = "mouse_down"
	ActionMouseUp       ActionKind = "mouse_up"
)

// Action is one computer-use request. Text is what to type, Label an exact
// accessibility label, Scope frontmost, dock, menubar, app:<name> or pid:<n>,
// Combo a key combo, Button left, right or middle, and Direction and Amount the
// scroll axis and wheel clicks, negative to reverse.
type Action struct {
	Kind      ActionKind
	Target    *Target
	Text      string
	Label     string
	Scope     string
	Combo     string
	Button    string
	Direction string
	Amount    int
}
