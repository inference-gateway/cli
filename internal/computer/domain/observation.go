package domain

// Observation is what the computer reports back after an action.
type Observation struct {
	Message  string // LLM-facing outcome description
	CursorX  int
	CursorY  int
	Width    int // frame-space dimensions
	Height   int
	Image    *Image      // set by screenshot actions
	Elements []UIElement // set by accessibility observations
}

// Image is a captured frame.
type Image struct {
	Data     string `json:"-"` // base64-encoded, in memory only - a record keeps Path
	MimeType string
	Path     string // on-disk copy, reachable by ImageDecode for non-vision models
}
