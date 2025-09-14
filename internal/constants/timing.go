package constants

import "time"

// TaskTransitionTiming contains constants for task state transition delays
// to improve UX when tasks transition from queued → working → completed
const (
	// A2A Task polling interval - how often to check task status
	A2ATaskPollInterval = 5 * time.Second

	// Agent service processing delays
	AgentIterationDelay        = 100 * time.Millisecond // Delay between agent iterations
	AgentToolExecutionDelay    = 100 * time.Millisecond // Delay during tool execution
	AgentParallelToolsDelay    = 100 * time.Millisecond // Delay for parallel tool coordination
	AgentConversationSaveDelay = 100 * time.Millisecond // Delay when saving conversations
	AgentStatusTickerInterval  = 200 * time.Millisecond // Status update ticker interval

	// UI component timing for smooth transitions
	ToolCallUpdateThrottle    = 50 * time.Millisecond  // Minimum time between tool call updates
	ToolCallMinShowTime       = 400 * time.Millisecond // Minimum time to show tool call before hiding
	ParallelToolsTickInterval = 500 * time.Millisecond // Parallel tools UI refresh interval

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
