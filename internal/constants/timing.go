package constants

import "time"

// TaskTransitionTiming contains constants for task state transition delays
// to improve UX when tasks transition from queued → working → completed
const (
	// Agent service processing delays
	AgentIterationDelay       = 20 * time.Millisecond  // Delay between agent iterations
	AgentToolExecutionDelay   = 20 * time.Millisecond  // Delay during tool execution
	AgentStatusTickerInterval = 200 * time.Millisecond // Status update ticker interval

	// UI component timing for smooth transitions
	ToolCallUpdateThrottle    = 50 * time.Millisecond  // Minimum time between tool call updates
	ToolCallMinShowTime       = 400 * time.Millisecond // Minimum time to show tool call before hiding
	ParallelToolsTickInterval = 500 * time.Millisecond // Parallel tools UI refresh interval
	TimerUpdateThrottle       = 100 * time.Millisecond // Minimum time between timer updates (e.g., bash command duration)
	RenderThrottleInterval    = 33 * time.Millisecond  // Throttle interval for streaming content rendering (~30 FPS)

	// Test timing delays
	TestSleepDelay = 100 * time.Millisecond // Standard delay in tests for timing-sensitive operations
)

// UITransitionTiming contains timing constants for UI state transitions
const (
	// Minimum display times for better UX
	MinToolDisplayTime    = 400 * time.Millisecond
	ToolUpdateThrottle    = 50 * time.Millisecond
	SpinnerUpdateInterval = 200 * time.Millisecond
	StatusRefreshInterval = 500 * time.Millisecond
)

// ObservabilityTiming contains thresholds for the single-ingress instrumentation:
// the Bubble Tea Update loop is single-threaded, so a slow handler is a UI freeze,
// and a background job that overruns is worth a one-time warning.
const (
	SlowUpdateThreshold     = 100 * time.Millisecond
	JobRunningLongThreshold = 5 * time.Minute
)
