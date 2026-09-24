package domain

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
// the max_duration cap, or shutdown).
type ScreenRecordingStatusEvent struct {
	Active bool
}
