package domain

// Observation is what the computer reports back after an action.
type Observation struct {
	Message string // LLM-facing outcome description
	CursorX int
	CursorY int
	Width   int // frame-space dimensions
	Height  int
	Image   *Image // set by screenshot actions
}

// Image is a captured frame.
type Image struct {
	Data     string // base64-encoded
	MimeType string
	Path     string // on-disk copy, reachable by ImageDecode for non-vision models
}
