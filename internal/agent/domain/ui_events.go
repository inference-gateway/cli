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
// bridge it into the rendered stream.
type ScreenRecordingStatusEvent struct {
	Active    bool
	Timestamp time.Time
}

func (e ScreenRecordingStatusEvent) GetRequestID() string    { return "" }
func (e ScreenRecordingStatusEvent) GetTimestamp() time.Time { return e.Timestamp }
