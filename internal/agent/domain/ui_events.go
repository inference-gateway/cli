package domain

import "time"

// ModelSelectedEvent is pushed through the UI notifier when something other
// than the TUI's own selector (e.g. the browser extension) switched the model,
// so an open selector can close and indicators refresh.
type ModelSelectedEvent struct {
	Model string
}

// BrowserExtensionStatusEvent is pushed through the UI notifier when the
// browser extension connects to or drops off the CLI bridge.
type BrowserExtensionStatusEvent struct {
	Connected bool
}

// ScreenRecordingStatusEvent is pushed through the UI notifier when a
// RecordStart screen recording begins or its ffmpeg process ends (RecordStop,
// the max_duration cap, or shutdown). It is also a ChatEvent so headless can
// bridge it into the rendered stream. A start carries the recording's output
// file and its rectangle in the frame space; an end carries none of that.
type ScreenRecordingStatusEvent struct {
	Active       bool
	Path         string
	RegionX      int
	RegionY      int
	RegionWidth  int
	RegionHeight int
	FrameWidth   int
	FrameHeight  int
	Timestamp    time.Time
}

func (e ScreenRecordingStatusEvent) GetRequestID() string    { return "" }
func (e ScreenRecordingStatusEvent) GetTimestamp() time.Time { return e.Timestamp }

// ComputerUseActionEvent is pushed through the UI notifier just before the
// Computer tool performs a pointer or keyboard action. Pointer actions carry
// x and y in screen coordinates; keyboard ones carry the screen size only, so
// a client renders the action without parsing tool payloads.
type ComputerUseActionEvent struct {
	ToolCallID   string
	Action       string
	X            int
	Y            int
	ScreenWidth  int
	ScreenHeight int
	Timestamp    time.Time
}

func (e ComputerUseActionEvent) GetRequestID() string    { return "" }
func (e ComputerUseActionEvent) GetTimestamp() time.Time { return e.Timestamp }
