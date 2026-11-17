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

// AnnotateOptions carries per-call annotation parameters. Prompt is the task
// instruction, "" for the annotator default. Width and Height are the image's
// pixel size, stated in the prompt and used to rescale normalized coordinates.
type AnnotateOptions struct {
	Prompt string
	Width  int
	Height int
}

// AnnotatedElement is one detected element of an annotated image. BBox is
// [x1, y1, x2, y2] in the image's pixel space.
type AnnotatedElement struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	Text  string `json:"text,omitempty"`
	BBox  [4]int `json:"bbox"`
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

// Frame is a captured image frame such as a screenshot or camera frame. Data is
// the base64 image, Path its on-disk file when there is one, Width and Height
// the size after scaling, Format "png" or "jpeg", and Method the capture method
// such as "x11" or "directory".
type Frame struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Data      string    `json:"data"`
	Path      string    `json:"-"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Format    string    `json:"format"`
	Method    string    `json:"method"`
}

// FrameSource provides the most recent frame of a named frame source
// (the screen ring buffer, a camera directory, ...).
type FrameSource interface {
	GetLatestFrame() (*Frame, error)
}

// FrameToolResult is the result of a frame retrieval. Note explains a degraded
// result, such as "annotation unavailable: ...".
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
	Note       string           `json:"note,omitempty"`
}
