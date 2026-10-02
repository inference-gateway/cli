package domain

import (
	"strings"
	"time"

	adk "github.com/inference-gateway/adk/types"
)

// TaskInfo wraps ADK Task with UI-specific metadata for completed/terminal tasks
// Used for A2A task retention and display
type TaskInfo struct {
	// ADK Task contains: ID, ContextID, Status (with State), History, Artifacts, Metadata
	Task adk.Task

	// UI-specific fields
	AgentURL    string
	StartedAt   time.Time
	CompletedAt time.Time
}

// TaskRetentionService manages in-memory retention of completed/terminal A2A tasks
// Only enabled when A2A is enabled - decouples task retention from the TUI state store
type TaskRetentionService interface {
	// AddTask adds a terminal task (completed, failed, canceled, etc.) to retention
	AddTask(task TaskInfo)

	// GetTasks returns all retained tasks
	GetTasks() []TaskInfo

	// Clear removes all retained tasks
	Clear()

	// SetMaxRetention updates the maximum retention count
	SetMaxRetention(maxRetention int)

	// GetMaxRetention returns the current maximum retention count
	GetMaxRetention() int
}

// BackgroundTaskService handles background A2A task operations
// Only enabled when A2A is enabled - provides task cancellation and retrieval
type BackgroundTaskService interface {
	// GetBackgroundTasks returns all current background polling tasks
	GetBackgroundTasks() []TaskPollingState

	// CancelBackgroundTask cancels a background task by task ID
	CancelBackgroundTask(taskID string) error
}

// Clearer forgets the A2A context/task graph and discards the in-flight A2A
// tasks. Conversation clear and switch call it, since the graph belongs to
// the conversation that is being left.
type Clearer interface {
	ClearAllAgents()
}

// TaskTracker handles A2A task ID and context ID tracking within chat
// sessions. Following A2A spec: supports multi-tenant with multiple
// contexts per agent.
type TaskTracker interface {
	// Context management (contexts are server-generated and tracked here).
	// Multiple contexts per agent enable multi-tenant/multi-session support.
	RegisterContext(agentURL, contextID string)
	GetLatestContextForAgent(agentURL string) string
	HasContext(contextID string) bool

	// Task management (tasks are server-generated and scoped to contexts per A2A spec)
	AddTask(contextID, taskID string)
	GetLatestTaskForContext(contextID string) string
	RemoveTask(taskID string)

	// Agent management
	Clearer

	// Polling state management (one polling state per task)
	StartPolling(taskID string, state *TaskPollingState)
	StopPolling(taskID string)
	GetPollingState(taskID string) *TaskPollingState
}

// TaskPollingState is the data record for one in-flight A2A task that the task
// view reads. Monitoring is owned by the job supervisor, which runs the task's
// background job that polls the remote agent.
type TaskPollingState struct {
	TaskID          string
	ContextID       string
	AgentURL        string
	TaskDescription string
	IsPolling       bool
	StartedAt       time.Time
	LastKnownState  string
}

var taskStates = []adk.TaskState{
	adk.TaskStateSubmitted,
	adk.TaskStateWorking,
	adk.TaskStateCompleted,
	adk.TaskStateFailed,
	adk.TaskStateCanceled,
	adk.TaskStateRejected,
	adk.TaskStateInputRequired,
	adk.TaskStateAuthRequired,
	adk.TaskStateUnspecified,
}

var taskStatesByKey = func() map[string]adk.TaskState {
	byKey := make(map[string]adk.TaskState, len(taskStates)+1)
	for _, state := range taskStates {
		byKey[taskStateKey(state)] = state
	}
	byKey["cancelled"] = adk.TaskStateCanceled
	return byKey
}()

// taskStateKey reduces a state to its bare, lower-case, underscore form, so
// TASK_STATE_INPUT_REQUIRED and input-required share the key input_required.
func taskStateKey(state adk.TaskState) string {
	key := strings.TrimPrefix(strings.ToLower(string(state)), "task_state_")
	return strings.ReplaceAll(key, "-", "_")
}

// NormalizeTaskState maps the state a remote agent reports to its
// adk.TaskState constant. Agents report the prefixed enum
// (TASK_STATE_COMPLETED) or a bare form such as completed, input-required or
// canceled. v1.0.1 renamed the cancelled spelling to canceled, so both are
// accepted on read. An unknown state is returned unchanged.
func NormalizeTaskState(state adk.TaskState) adk.TaskState {
	if normalized, ok := taskStatesByKey[taskStateKey(state)]; ok {
		return normalized
	}
	return state
}

// Task statuses as the task view names them. Running covers any task still in
// flight whose remote state is not known yet.
const (
	TaskStatusRunning       = "Running"
	TaskStatusSubmitted     = "Submitted"
	TaskStatusWorking       = "Working"
	TaskStatusCompleted     = "Completed"
	TaskStatusFailed        = "Failed"
	TaskStatusCanceled      = "Canceled"
	TaskStatusRejected      = "Rejected"
	TaskStatusInputRequired = "Input Required"
	TaskStatusAuthRequired  = "Auth Required"
	TaskStatusUnknown       = "Unknown"
)

var taskStateDisplayNames = map[adk.TaskState]string{
	adk.TaskStateSubmitted:     TaskStatusSubmitted,
	adk.TaskStateWorking:       TaskStatusWorking,
	adk.TaskStateCompleted:     TaskStatusCompleted,
	adk.TaskStateFailed:        TaskStatusFailed,
	adk.TaskStateCanceled:      TaskStatusCanceled,
	adk.TaskStateRejected:      TaskStatusRejected,
	adk.TaskStateInputRequired: TaskStatusInputRequired,
	adk.TaskStateAuthRequired:  TaskStatusAuthRequired,
	adk.TaskStateUnspecified:   TaskStatusUnknown,
}

// TaskStateDisplayName names a task state for display. Every adk.TaskState has
// a name. An unknown state is title-cased with its "TASK_STATE_" prefix
// stripped, so it never shows as a raw enum.
func TaskStateDisplayName(state string) string {
	if displayName, exists := taskStateDisplayNames[NormalizeTaskState(adk.TaskState(state))]; exists {
		return displayName
	}

	stateStr := strings.TrimPrefix(state, "TASK_STATE_")
	stateStr = strings.ReplaceAll(strings.ToLower(stateStr), "_", " ")
	if stateStr == "" {
		return TaskStatusUnknown
	}
	return strings.ToUpper(stateStr[:1]) + stateStr[1:]
}
