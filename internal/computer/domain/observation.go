package domain

// Observation is what the computer reports back after an action: an LLM-facing
// Message, the cursor, the frame-space size, and the Image of a screenshot or
// the Elements of an accessibility observation.
type Observation struct {
	Message  string
	CursorX  int
	CursorY  int
	Width    int
	Height   int
	Image    *Image
	Elements []UIElement
}

// Image is a captured frame. Data is base64 and kept in memory only, while Path
// is the on-disk copy ImageDecode reads for non-vision models.
type Image struct {
	Data     string `json:"-"`
	MimeType string
	Path     string
}
