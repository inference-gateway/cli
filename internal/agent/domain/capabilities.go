// Capability contracts and result types: images, screen frames, and browser automation.

package domain

import (
	"context"
	"time"
)

// ImageAttachment represents an image attachment in a message
type ImageAttachment struct {
	Data        string `json:"data"`
	MimeType    string `json:"mime_type"`
	Filename    string `json:"filename,omitempty"`
	DisplayName string `json:"display_name"`
	SourcePath  string `json:"-"`
}

// AnnotateOptions carries per-call annotation parameters.
type AnnotateOptions struct {
	Prompt string // task instruction (UI-element detection, scene description, or a user question); "" -> annotator default
	Width  int    // image width in pixels, stated in the prompt and used to rescale normalized coordinates
	Height int    // image height in pixels
}

// AnnotatedElement is one detected element of an annotated image.
type AnnotatedElement struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	Text  string `json:"text,omitempty"`
	BBox  [4]int `json:"bbox"` // [x1, y1, x2, y2] in the image's pixel space
}

// ImageAnnotation is the structured result of annotating an image: a short
// scene summary plus a numbered element list. Elements may be empty when the
// annotator degraded to a plain-text summary.
type ImageAnnotation struct {
	Summary  string             `json:"summary"`
	Elements []AnnotatedElement `json:"elements,omitempty"`
}

// ImageAnnotator turns an image into text (summary + element list) via a
// vision model, so text-only session models can understand frames and images.
type ImageAnnotator interface {
	AnnotateImage(ctx context.Context, img ImageAttachment, opts AnnotateOptions) (*ImageAnnotation, error)
}

// ScreenRegion represents a rectangular region of the screen
type ScreenRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Frame represents a captured image frame (screenshot, camera frame, ...) with metadata
type Frame struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Data      string    `json:"data"`   // base64 encoded image
	Path      string    `json:"-"`      // on-disk path when the frame exists as a file
	Width     int       `json:"width"`  // Final image width (after scaling)
	Height    int       `json:"height"` // Final image height (after scaling)
	Format    string    `json:"format"` // "png" or "jpeg"
	Method    string    `json:"method"` // capture method, e.g. "x11", "wayland", "directory"
}

// FrameSource provides the most recent frame of a named frame source
// (the screen ring buffer, a camera directory, ...).
type FrameSource interface {
	GetLatestFrame() (*Frame, error)
}

// FrameToolResult represents the result of a frame retrieval
type FrameToolResult struct {
	Source     string           `json:"source,omitempty"`
	Display    string           `json:"display"`
	Region     *ScreenRegion    `json:"region,omitempty"`
	Width      int              `json:"width"`
	Height     int              `json:"height"`
	Format     string           `json:"format"`
	Method     string           `json:"method"`
	Annotated  bool             `json:"annotated,omitempty"`
	Annotation *ImageAnnotation `json:"annotation,omitempty"`
	Note       string           `json:"note,omitempty"` // degrade note, e.g. "annotation unavailable: ..."
}
