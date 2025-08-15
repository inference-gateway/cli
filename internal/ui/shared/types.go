package shared

// KeyShortcut represents a keyboard shortcut with description
type KeyShortcut struct {
	Key         string
	Description string
}

// ScrollDirection represents different scroll directions
type ScrollDirection int

const (
	ScrollUp ScrollDirection = iota
	ScrollDown
	ScrollToTop
	ScrollToBottom
)