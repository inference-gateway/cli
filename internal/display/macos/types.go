//go:build darwin

package macos

// PauseResumeRequest represents a pause or resume request from the Swift window
type PauseResumeRequest struct {
	Action    string `json:"action"`
	RequestID string `json:"request_id"`
}

// WindowEvent wraps domain.ChatEvent with additional metadata for the window
type WindowEvent struct {
	Type      string `json:"type"`
	Timestamp int64  `json:"timestamp"`
	Data      any    `json:"data"`
}

// WindowState represents the current state snapshot of the floating window
// Used for reconnection and state synchronization
type WindowState struct {
	SessionID     string `json:"session_id"`
	IsActive      bool   `json:"is_active"`
	CurrentStatus string `json:"current_status"`
	ActivityCount int    `json:"activity_count"`
}
