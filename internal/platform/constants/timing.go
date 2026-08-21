package constants

import "time"

// TaskTransitionTiming contains constants for task state transition delays
// to improve UX when tasks transition from queued → working → completed
const (
	// Agent service processing delays
	AgentIterationDelay       = 20 * time.Millisecond  // Delay between agent iterations
	AgentToolExecutionDelay   = 20 * time.Millisecond  // Delay during tool execution
	AgentStatusTickerInterval = 200 * time.Millisecond // Status update ticker interval
	DrainQueueRetryInterval   = 300 * time.Millisecond // Re-check interval while queued work waits behind a busy agent

	// UI component timing for smooth transitions
	ToolCallUpdateThrottle = 50 * time.Millisecond // Minimum time between tool call updates

	// Test timing delays
	TestSleepDelay = 100 * time.Millisecond // Standard delay in tests for timing-sensitive operations
)

// ObservabilityTiming contains thresholds for the single-ingress instrumentation:
// the Bubble Tea Update loop is single-threaded, so a slow handler is a UI freeze,
// and a background job that overruns is worth a one-time warning.
const (
	SlowUpdateThreshold     = 100 * time.Millisecond
	JobRunningLongThreshold = 5 * time.Minute
)

// GitCommandTimeout bounds ad-hoc git shells (branch lookups, log, rev-parse)
// so a wedged git process can't hang the UI or system-prompt build.
const GitCommandTimeout = 10 * time.Second

// ApprovalTimeout is how long the agent waits for a tool-approval decision
// before auto-rejecting, in every delivery path (chat TUI prompt, headless
// IPC under the daemon, and channel auto-reject).
const ApprovalTimeout = 5 * time.Minute
