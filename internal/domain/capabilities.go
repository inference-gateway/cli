// Capability contracts and result types: images, screen frames, and browser automation.

package domain

import (
	"context"
	"time"

	sdk "github.com/inference-gateway/sdk"
)

// Assistant message
type Message = sdk.Message

// Common role constants
const (
	RoleUser      = sdk.User
	RoleAssistant = sdk.Assistant
	RoleTool      = sdk.Tool
	RoleSystem    = sdk.System
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

// Application represents a running application on any platform.
// ID is a stable cross-platform identifier ("pid:N" on macOS and Linux, or
// the app name for name-based lookup).
// Name is the human-readable display name.
// PlatformID is the OS-native identifier (the PID on macOS and Linux, window ID on X11).
type Application struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PlatformID string `json:"platform_id"`
}

// Computer use result types

// ScreenRegion represents a rectangular region of the screen
type ScreenRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Frame represents a captured image frame (screenshot, camera frame, ...) with metadata
type Frame struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	Data           string    `json:"data"`            // base64 encoded image
	Path           string    `json:"-"`               // on-disk path when the frame exists as a file
	Width          int       `json:"width"`           // Final image width (after scaling)
	Height         int       `json:"height"`          // Final image height (after scaling)
	Format         string    `json:"format"`          // "png" or "jpeg"
	Method         string    `json:"method"`          // capture method, e.g. "x11", "wayland", "directory"
	OriginalWidth  int       `json:"original_width"`  // Screen width before scaling
	OriginalHeight int       `json:"original_height"` // Screen height before scaling
}

// FrameSource provides the most recent frame of a named frame source
// (the screen ring buffer, a camera directory, ...).
type FrameSource interface {
	GetLatestFrame() (*Frame, error)
}

// RateLimiter defines the interface for rate limiting computer use actions
type RateLimiter interface {
	// CheckAndRecord checks if the action is within rate limits and records it
	CheckAndRecord(toolName string) error
	// GetCurrentCount returns the number of actions in the current window
	GetCurrentCount() int
	// Reset clears all recorded actions
	Reset()
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

// MouseMoveToolResult represents the result of a mouse move operation
type MouseMoveToolResult struct {
	FromX   int    `json:"from_x"`
	FromY   int    `json:"from_y"`
	ToX     int    `json:"to_x"`
	ToY     int    `json:"to_y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// MouseClickToolResult represents the result of a mouse click operation
type MouseClickToolResult struct {
	Button  string `json:"button"`
	Clicks  int    `json:"clicks"`
	X       int    `json:"x"`
	Y       int    `json:"y"`
	Display string `json:"display"`
	Method  string `json:"method"`
}

// KeyboardTypeToolResult represents the result of a keyboard input operation
type KeyboardTypeToolResult struct {
	Text     string `json:"text,omitempty"`
	KeyCombo string `json:"key_combo,omitempty"`
	Display  string `json:"display"`
	Method   string `json:"method"`
}

// BrowserToolResult represents the result of a browser use operation. One
// shared shape for all browser tools; each tool fills the fields it produces.
type BrowserToolResult struct {
	Action   string   `json:"action"`
	URL      string   `json:"url,omitempty"`
	Title    string   `json:"title,omitempty"`
	Selector string   `json:"selector,omitempty"`
	Text     string   `json:"text,omitempty"`
	Content  string   `json:"content,omitempty"`
	Events   []string `json:"events,omitempty"`
}

// BrowserTab describes one open tab/page as reported by BrowserTabs. Active
// marks the tab the browser-use verbs currently act on.
type BrowserTab struct {
	Index  int    `json:"index"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
}

// BrowserScreenshotResult carries a screenshot of the current page. Data is
// base64-encoded image bytes; the BrowserScreenshot tool persists them and
// sets the attachment's on-disk source path.
type BrowserScreenshotResult struct {
	Data     string `json:"-"`
	MimeType string `json:"mime_type"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
}

// BrowserDriver executes browser-use verbs against a browser backend: a
// Playwright-launched browser, or the user's real browser via the opentask
// extension bridge.
type BrowserDriver interface {
	Navigate(ctx context.Context, url string) (BrowserToolResult, error)
	Click(ctx context.Context, selector string) (BrowserToolResult, error)
	// ClickAt clicks at viewport coordinates (CSS pixels), for use with a
	// screenshot. Not every backend supports it (the extension bridge does not).
	ClickAt(ctx context.Context, x, y float64) (BrowserToolResult, error)
	Type(ctx context.Context, selector, text string, pressEnter bool) (BrowserToolResult, error)
	// Read returns element text in Content and drained browser events in Events.
	// Sensitive input values (passwords, tokens) MUST be redacted before return.
	Read(ctx context.Context, selector string) (BrowserToolResult, error)
	// Screenshot captures the current page/active tab as an image.
	Screenshot(ctx context.Context) (BrowserScreenshotResult, error)
	// Tabs lists the open tabs, marking the active one.
	Tabs(ctx context.Context) ([]BrowserTab, error)
	Close()
}
