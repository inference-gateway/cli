package domain

// UIElement is one compact accessibility-tree observation. BBox uses the
// same frame coordinate space as Computer screenshots and pointer actions.
type UIElement struct {
	Role  string `json:"role"`
	Label string `json:"label"`
	State string `json:"state"`
	BBox  [4]int `json:"bbox"`
}
