package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	baggage "go.opentelemetry.io/otel/baggage"
	trace "go.opentelemetry.io/otel/trace"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	download "github.com/inference-gateway/cli/internal/platform/download"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	a2adomain "github.com/inference-gateway/cli/internal/protocols/a2a/domain"
	a2ainfra "github.com/inference-gateway/cli/internal/protocols/a2a/infrastructure"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
	tools "github.com/inference-gateway/cli/internal/tools"
)

// SubmitTaskTool handles A2A task submission and management
type SubmitTaskTool struct {
	config      *config.Config
	formatter   tools.CustomFormatter
	taskTracker a2adomain.TaskTracker
	submitter   scheddomain.JobSubmitter
	retention   a2adomain.TaskRetentionService
	client      client.A2AClient
}

// SubmitTaskResult represents the result of an A2A task operation
type SubmitTaskResult struct {
	TaskID     string    `json:"task_id"`
	ContextID  string    `json:"context_id,omitempty"`
	AgentURL   string    `json:"agent_url"`
	State      string    `json:"state"`
	Message    string    `json:"message"`
	TaskResult string    `json:"task_result,omitempty"`
	Success    bool      `json:"success"`
	Task       *adk.Task `json:"task,omitempty"`
}

// NewSubmitTaskTool creates a new A2A task tool
func NewSubmitTaskTool(cfg *config.Config, taskTracker a2adomain.TaskTracker, submitter scheddomain.JobSubmitter, retention a2adomain.TaskRetentionService) *SubmitTaskTool {
	return &SubmitTaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		submitter:   submitter,
		retention:   retention,
		client:      nil,
		formatter: tools.NewCustomFormatter(ToolSubmitTask, func(key string) bool {
			return key == "metadata"
		}),
	}
}

// NewSubmitTaskToolWithClient creates a new A2A task tool with an injected client (for testing)
func NewSubmitTaskToolWithClient(cfg *config.Config, taskTracker a2adomain.TaskTracker, submitter scheddomain.JobSubmitter, retention a2adomain.TaskRetentionService, client client.A2AClient) *SubmitTaskTool {
	return &SubmitTaskTool{
		config:      cfg,
		taskTracker: taskTracker,
		submitter:   submitter,
		retention:   retention,
		client:      client,
		formatter: tools.NewCustomFormatter(ToolSubmitTask, func(key string) bool {
			return key == "metadata"
		}),
	}
}

// shouldResumeTask checks if an existing task should be resumed (returns task state and whether to resume)
func (t *SubmitTaskTool) shouldResumeTask(ctx context.Context, adkClient client.A2AClient, existingTaskID string) (adk.TaskState, bool, error) {
	if existingTaskID == "" {
		return "", false, nil
	}

	queryParams := adk.GetTaskRequest{ID: existingTaskID}
	taskStatus, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		return "", false, nil
	}

	if taskStatus == nil {
		return "", false, nil
	}

	var existingTask adk.Task
	if err := mapToStruct(taskStatus.Result, &existingTask); err != nil {
		return "", false, err
	}

	return existingTask.Status.State, true, nil
}

// Manifest returns the tool's manifest with its configured require_approval.
func (t *SubmitTaskTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolSubmitTask).WithRequireApproval(t.config.A2A.Tools.SubmitTask.RequireApproval)
}

// Definition returns the tool definition for the LLM
func (t *SubmitTaskTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute submits a task to an A2A agent. The agent's latest tracked task is
// resumed only when it is input-required; otherwise the message opens a fresh
// context so independent tasks on the same agent run in parallel. A caller
// continues an earlier conversation deliberately by passing its context_id.
//
//nolint:gocyclo,cyclop,funlen
func (t *SubmitTaskTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	startTime := time.Now()

	if !t.IsEnabled() {
		return &agentdomain.ToolExecutionResult{
			ToolName:  ToolSubmitTask,
			Arguments: args,
			Success:   false,
			Duration:  time.Since(startTime),
			Error:     "A2A connections are disabled in configuration",
			Data: SubmitTaskResult{
				Success: false,
				Message: "A2A connections are disabled",
			},
		}, nil
	}

	agentURL, ok := args["agent_url"].(string)
	if !ok {
		return t.errorResult(args, startTime, "agent_url parameter is required and must be a string")
	}

	taskDescription, ok := args["task_description"].(string)
	if !ok {
		return t.errorResult(args, startTime, "task_description parameter is required and must be a string")
	}

	requestedContextID, _ := args["context_id"].(string)
	existingContextID := requestedContextID
	var existingTaskID string
	if t.taskTracker != nil {
		if existingContextID == "" {
			existingContextID = t.taskTracker.GetLatestContextForAgent(agentURL)
		}
		if existingContextID != "" {
			existingTaskID = t.taskTracker.GetLatestTaskForContext(existingContextID)
		}
	}

	var adkClient client.A2AClient
	if t.client != nil {
		adkClient = t.client
	} else {
		adkClient = a2ainfra.NewClient(agentURL)
	}

	taskState, _, _ := t.shouldResumeTask(ctx, adkClient, existingTaskID)
	shouldResume := taskState == adk.TaskStateInputRequired

	message := adk.Message{
		MessageID: fmt.Sprintf("user-message-%d", time.Now().UnixNano()),
		Role:      adk.RoleUser,
		Parts: []adk.Part{
			adk.NewTextPart(taskDescription),
		},
	}

	if shouldResume {
		message.TaskID = &existingTaskID
	}

	switch {
	case shouldResume, requestedContextID != "":
		message.ContextID = &existingContextID
	default:
		existingTaskID = ""
	}

	returnImmediately := true
	sendRequest := adk.SendMessageRequest{
		Message: message,
		Configuration: &adk.SendMessageConfiguration{
			AcceptedOutputModes: []string{"text"},
			ReturnImmediately:   &returnImmediately,
		},
	}

	taskResponse, err := adkClient.SendTask(ctx, sendRequest)
	if err != nil {
		shouldClear := t.taskTracker != nil && existingTaskID != "" && t.isTaskNotFoundError(err)
		if shouldClear {
			t.taskTracker.RemoveTask(existingTaskID)
			return t.errorResult(args, startTime, fmt.Sprintf("Previous task no longer exists (cleared from tracker): %v", err))
		}
		return t.errorResult(args, startTime, fmt.Sprintf("A2A task submission failed: %v", err))
	}

	var submittedTask adk.Task
	if err := mapToStruct(taskResponse.Result, &submittedTask); err != nil {
		return t.errorResult(args, startTime, "Failed to parse task submission response")
	}

	if submittedTask.ID == "" {
		return t.errorResult(args, startTime, "Task submitted but no task ID received")
	}

	taskID := submittedTask.ID
	receivedContextID := submittedTask.GetContextID()

	if t.taskTracker != nil && receivedContextID != "" {
		if !t.taskTracker.HasContext(receivedContextID) {
			t.taskTracker.RegisterContext(agentURL, receivedContextID)
		}

		isCompleted := submittedTask.Status.State == adk.TaskStateCompleted
		isFailed := submittedTask.Status.State == adk.TaskStateFailed
		if shouldResume && (isCompleted || isFailed) {
			t.taskTracker.RemoveTask(existingTaskID)
			return t.errorResult(args, startTime, fmt.Sprintf("Previous task %s is already %s (cleared from tracker)", existingTaskID, submittedTask.Status.State))
		}

		if !shouldResume {
			t.taskTracker.AddTask(receivedContextID, taskID)
		}
	}

	pollingState := &a2adomain.TaskPollingState{
		TaskID:          taskID,
		ContextID:       receivedContextID,
		AgentURL:        agentURL,
		TaskDescription: taskDescription,
		IsPolling:       false,
		StartedAt:       time.Now(),
	}

	if t.taskTracker != nil {
		t.taskTracker.StartPolling(taskID, pollingState)
	}

	if t.submitter != nil {
		t.submitter.Submit(&a2aJob{
			tool:     t,
			agentURL: agentURL,
			taskID:   taskID,
			state:    pollingState,
			spanCtx:  trace.SpanContextFromContext(ctx),
			bag:      baggage.FromContext(ctx),
		})
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSubmitTask,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(startTime),
		Data: SubmitTaskResult{
			TaskID:     submittedTask.ID,
			ContextID:  submittedTask.GetContextID(),
			AgentURL:   agentURL,
			State:      string(submittedTask.Status.State),
			Success:    true,
			Message:    fmt.Sprintf("Task delegated to %s and monitoring in background", agentURL),
			TaskResult: fmt.Sprintf("Task %s delegated successfully. The completion notification will be injected into the conversation automatically - do not poll with A2A_QueryTask and do not block on the Wait tool; work on something else or end your turn.", taskID),
		},
	}, nil
}

// runA2APolling polls the remote agent until the task reaches a terminal state
// and returns the outcome. It is the body of a2aJob.Run - the supervisor owns
// the goroutine - so it returns the result and emits intermediate status via emit
// rather than pushing onto channels (no producer/consumer split, hence no
// ordering sleeps). StopPolling fires on every exit so the task view stops
// showing it as active.
func (t *SubmitTaskTool) runA2APolling(
	ctx context.Context,
	agentURL string,
	taskID string,
	state *a2adomain.TaskPollingState,
	emit func(scheddomain.JobSignal),
	observe func(adk.Task),
) agentdomain.ToolExecutionResult {
	if t.taskTracker != nil {
		defer t.taskTracker.StopPolling(taskID)
	}

	adkClient := t.getOrCreateClient(agentURL)

	strategy := t.config.A2A.Task.PollingStrategy
	currentInterval := t.initializePollingStrategy(agentURL, taskID, strategy)

	pollAttempt := 0
	var pollingDetails strings.Builder

	ticker := time.NewTicker(currentInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return agentdomain.ToolExecutionResult{
				ToolName: ToolSubmitTask,
				Success:  false,
				Error:    "task cancelled",
				Data: SubmitTaskResult{
					TaskID:    taskID,
					ContextID: state.ContextID,
					AgentURL:  agentURL,
					State:     string(adk.TaskStateCanceled),
					Success:   false,
					Message:   "Task was canceled",
				},
			}

		case <-ticker.C:
			pollAttempt++
			fmt.Fprintf(&pollingDetails, "Poll #%d: interval=%v, elapsed=%v\n",
				pollAttempt, currentInterval, time.Since(state.StartedAt))

			currentTask, err := t.queryTask(ctx, adkClient, taskID)
			if err != nil || currentTask == nil {
				currentInterval = t.handleQueryError(agentURL, taskID, strategy, currentInterval, state, ticker, err)
				continue
			}

			observe(*currentTask)
			t.emitStatusUpdate(state, taskID, agentURL, *currentTask, emit)

			shouldReturn, taskResult := t.handleTaskState(ctx, agentURL, taskID, pollAttempt, state, *currentTask, pollingDetails.String())
			if shouldReturn {
				if taskResult != nil {
					return *taskResult
				}
				return agentdomain.ToolExecutionResult{ToolName: ToolSubmitTask, Success: false, Error: "task ended without a result"}
			}

			currentInterval = t.applyExponentialBackoff(agentURL, taskID, strategy, currentInterval, pollAttempt, state, ticker)
		}
	}
}

func (t *SubmitTaskTool) getOrCreateClient(agentURL string) client.A2AClient {
	if t.client != nil {
		return t.client
	}
	return a2ainfra.NewClient(agentURL)
}

func (t *SubmitTaskTool) initializePollingStrategy(_ /* agentURL */, _ /* taskID */, strategy string) time.Duration {
	var currentInterval time.Duration

	if strategy == "exponential" {
		currentInterval = time.Duration(t.config.A2A.Task.InitialPollIntervalSec) * time.Second
	} else {
		currentInterval = time.Duration(t.config.A2A.Task.StatusPollSeconds) * time.Second
	}

	return currentInterval
}

func (t *SubmitTaskTool) queryTask(ctx context.Context, adkClient client.A2AClient, taskID string) (*adk.Task, error) {
	queryParams := adk.GetTaskRequest{ID: taskID}
	taskStatus, err := adkClient.GetTask(ctx, queryParams)
	if err != nil {
		return nil, err
	}

	var currentTask adk.Task
	if err := mapToStruct(taskStatus.Result, &currentTask); err != nil {
		return nil, err
	}

	return &currentTask, nil
}

func (t *SubmitTaskTool) handleQueryError(_ /* agentURL */, _ /* taskID */ string, strategy string, currentInterval time.Duration, _ /* state */ *a2adomain.TaskPollingState, ticker *time.Ticker, _ /* err */ error) time.Duration {
	if strategy != "exponential" {
		return currentInterval
	}

	newInterval := time.Duration(float64(currentInterval) * t.config.A2A.Task.BackoffMultiplier)
	maxInterval := time.Duration(t.config.A2A.Task.MaxPollIntervalSec) * time.Second
	if newInterval > maxInterval {
		newInterval = maxInterval
	}

	ticker.Reset(newInterval)

	return newInterval
}

// extractTextFromParts extracts text content from ADK message parts
func (t *SubmitTaskTool) extractTextFromParts(parts []adk.Part) string {
	return textFromParts(parts)
}

// emitStatusUpdate records the latest remote task state on the polling state
// (read by the task view) and emits it as a non-terminal JobSignal for the UI.
func (t *SubmitTaskTool) emitStatusUpdate(state *a2adomain.TaskPollingState, _, agentURL string, currentTask adk.Task, emit func(scheddomain.JobSignal)) {
	statusMessage := ""
	if currentTask.Status.Message != nil {
		statusMessage = t.extractTextFromParts(currentTask.Status.Message.Parts)
	}

	stateStr := string(currentTask.Status.State)
	state.LastKnownState = stateStr

	note := fmt.Sprintf("A2A task on %s: %s", agentURL, stateStr)
	if statusMessage != "" {
		note = fmt.Sprintf("A2A task on %s (%s): %s", agentURL, stateStr, statusMessage)
	}
	if emit != nil {
		emit(scheddomain.JobSignal{Note: note, State: stateStr})
	}
}

func (t *SubmitTaskTool) handleTaskState(ctx context.Context, agentURL, _ /* taskID */ string, _ /* pollAttempt */ int, state *a2adomain.TaskPollingState, currentTask adk.Task, _ /* pollingDetails */ string) (bool, *agentdomain.ToolExecutionResult) {
	switch a2adomain.NormalizeTaskState(currentTask.Status.State) {
	case adk.TaskStateCompleted:
		finalResult := ""
		if currentTask.Status.Message != nil {
			finalResult = t.extractTextFromParts(currentTask.Status.Message.Parts)
		}

		if t.config.A2A.Task.ArtifactsAutoDownload {
			t.downloadArtifacts(ctx, &currentTask)
		}

		result := &agentdomain.ToolExecutionResult{
			ToolName: ToolSubmitTask,
			Success:  true,
			Duration: time.Since(state.StartedAt),
			Data: SubmitTaskResult{
				TaskID:     currentTask.ID,
				ContextID:  currentTask.GetContextID(),
				AgentURL:   agentURL,
				State:      string(currentTask.Status.State),
				Success:    true,
				Message:    fmt.Sprintf("Task %s", currentTask.Status.State),
				TaskResult: finalResult,
				Task:       &currentTask,
			},
		}
		return true, result

	case adk.TaskStateFailed:
		finalResult := failureReasonFromTask(currentTask)

		msg := fmt.Sprintf("Task %s", currentTask.Status.State)
		if finalResult != "" {
			msg = fmt.Sprintf("Task %s: %s", currentTask.Status.State, finalResult)
		}

		result := &agentdomain.ToolExecutionResult{
			ToolName: ToolSubmitTask,
			Success:  false,
			Duration: time.Since(state.StartedAt),
			Error:    finalResult,
			Data: SubmitTaskResult{
				TaskID:     currentTask.ID,
				ContextID:  currentTask.GetContextID(),
				AgentURL:   agentURL,
				State:      string(currentTask.Status.State),
				Success:    false,
				Message:    msg,
				TaskResult: finalResult,
				Task:       &currentTask,
			},
		}
		return true, result

	case adk.TaskStateInputRequired:
		inputMessage := ""
		if currentTask.Status.Message != nil {
			inputMessage = t.extractTextFromParts(currentTask.Status.Message.Parts)
		}

		result := &agentdomain.ToolExecutionResult{
			ToolName: ToolSubmitTask,
			Success:  true,
			Duration: time.Since(state.StartedAt),
			Data: SubmitTaskResult{
				TaskID:     currentTask.ID,
				ContextID:  currentTask.GetContextID(),
				AgentURL:   agentURL,
				State:      string(currentTask.Status.State),
				Success:    true,
				Message:    fmt.Sprintf("Task requires input: %s", inputMessage),
				TaskResult: inputMessage,
			},
		}
		return true, result

	case adk.TaskStateCanceled:
		cancelMessage := ""
		if currentTask.Status.Message != nil {
			cancelMessage = t.extractTextFromParts(currentTask.Status.Message.Parts)
		}

		result := &agentdomain.ToolExecutionResult{
			ToolName: ToolSubmitTask,
			Success:  false,
			Duration: time.Since(state.StartedAt),
			Data: SubmitTaskResult{
				TaskID:     currentTask.ID,
				ContextID:  currentTask.GetContextID(),
				AgentURL:   agentURL,
				State:      string(currentTask.Status.State),
				Success:    false,
				Message:    fmt.Sprintf("Task was canceled: %s", cancelMessage),
				TaskResult: cancelMessage,
			},
		}
		return true, result
	}

	return false, nil
}

func (t *SubmitTaskTool) applyExponentialBackoff(_ /* agentURL */, _ /* taskID */ string, strategy string, currentInterval time.Duration, _ /* pollAttempt */ int, _ /* state */ *a2adomain.TaskPollingState, ticker *time.Ticker) time.Duration {
	if strategy != "exponential" {
		return currentInterval
	}

	newInterval := time.Duration(float64(currentInterval) * t.config.A2A.Task.BackoffMultiplier)
	maxInterval := time.Duration(t.config.A2A.Task.MaxPollIntervalSec) * time.Second
	if newInterval > maxInterval {
		newInterval = maxInterval
	}

	ticker.Reset(newInterval)

	return newInterval
}

// Validate checks if the tool arguments are valid
func (t *SubmitTaskTool) Validate(args map[string]any) error {
	if _, ok := args["agent_url"].(string); !ok {
		return fmt.Errorf("agent_url parameter is required and must be a string")
	}
	if _, ok := args["task_description"].(string); !ok {
		return fmt.Errorf("task_description parameter is required and must be a string")
	}
	return nil
}

// IsEnabled returns whether this tool is enabled
func (t *SubmitTaskTool) IsEnabled() bool {
	return t.config.IsA2AToolsEnabled() && t.config.A2A.Tools.SubmitTask.Enabled
}

// SelfGated implements agentdomain.SelfGatedTool: a2a.enabled, not
// tools.enabled, switches the A2A tools on.
func (t *SubmitTaskTool) SelfGated() bool { return true }

// FormatResult formats tool execution results for different contexts
func (t *SubmitTaskTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterUI:
		return t.FormatForUI(result)
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatForLLM formats the result for LLM consumption with detailed information
func (t *SubmitTaskTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	var dataContent string
	if result.Data != nil {
		dataContent, _ = t.formatA2ATaskData(result.Data, result.Metadata)
	}
	return t.formatter.FormatExpanded(result, dataContent)
}

// formatA2ATaskData formats the task data content and returns it along with metadata presence
func (t *SubmitTaskTool) formatA2ATaskData(data any, metadata map[string]string) (string, bool) {
	taskData, ok := data.(SubmitTaskResult)
	if !ok {
		dataContent := t.formatter.FormatAsJSON(data)
		hasMetadata := len(metadata) > 0
		return dataContent, hasMetadata
	}

	var dataContent strings.Builder
	fmt.Fprintf(&dataContent, "Task ID: %s\n", taskData.TaskID)
	if taskData.ContextID != "" {
		fmt.Fprintf(&dataContent, "Context ID: %s\n", taskData.ContextID)
	}
	fmt.Fprintf(&dataContent, "State: %s\n", taskData.State)

	t.appendTaskMetadataLines(&dataContent, taskData.Task)

	if a2adomain.NormalizeTaskState(adk.TaskState(taskData.State)) == adk.TaskStateFailed {
		reason := taskData.TaskResult
		if reason == "" && taskData.Task != nil {
			reason = failureReasonFromTask(*taskData.Task)
		}
		if reason != "" {
			fmt.Fprintf(&dataContent, "\nFailure reason: %s\n", reason)
		}
	} else if taskData.TaskResult != "" {
		fmt.Fprintf(&dataContent, "\n%s", taskData.TaskResult)
	}

	if taskData.Task != nil && len(taskData.Task.Artifacts) > 0 {
		fmt.Fprintf(&dataContent, "\n\nArtifacts Available: %d\n", len(taskData.Task.Artifacts))
		downloaded := false
		for i, artifact := range taskData.Task.Artifacts {
			t.formatArtifact(&dataContent, i+1, artifact)
			if artifactLocalPath(artifact) != "" {
				downloaded = true
			}
		}
		if downloaded {
			dataContent.WriteString("\nArtifacts marked \"Saved to\" are already downloaded locally - use those paths directly, no need to fetch them.\n")
		} else {
			dataContent.WriteString("\nTo download artifacts: Use WebFetch tool with the Download URL from each artifact above.\n")
		}
	}

	hasMetadata := len(metadata) > 0
	return dataContent.String(), hasMetadata
}

// appendTaskMetadataLines writes "Usage:" / "Execution Stats:" lines
// from Task.Metadata into the formatted result, so the persisted history
// view shows per-task token consumption and tool counts. Silently no-ops
// when the remote agent didn't attach metadata (older ADK, or
// EnableUsageMetadata=false).
func (t *SubmitTaskTool) appendTaskMetadataLines(builder *strings.Builder, task *adk.Task) {
	if task == nil || task.Metadata == nil {
		return
	}
	meta := *task.Metadata

	if usageLine := formatMetadataMap(meta, "usage"); usageLine != "" {
		fmt.Fprintf(builder, "Usage: %s\n", usageLine)
	}
	if statsLine := formatMetadataMap(meta, "execution_stats"); statsLine != "" {
		fmt.Fprintf(builder, "Execution Stats: %s\n", statsLine)
	}
}

// formatMetadataMap returns a flat "k1=v1, k2=v2" representation of one
// top-level metadata map field. Keys are sorted for stable output.
// Returns "" if the field is absent or empty.
func formatMetadataMap(meta map[string]any, field string) string {
	raw, ok := meta[field]
	if !ok || raw == nil {
		return ""
	}
	m, ok := raw.(map[string]any)
	if !ok || len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

// formatArtifact formats a single artifact for display
func (t *SubmitTaskTool) formatArtifact(builder *strings.Builder, index int, artifact adk.Artifact) {
	artifactName := "unnamed"
	if artifact.Name != nil {
		artifactName = *artifact.Name
	}
	fmt.Fprintf(builder, "%d. %s (ID: %s)", index, artifactName, artifact.ArtifactID)

	if artifact.Metadata != nil {
		if mimeType, ok := (*artifact.Metadata)["mime_type"].(string); ok {
			fmt.Fprintf(builder, ", Type: %s", mimeType)
		}
		if size, ok := (*artifact.Metadata)["size"].(float64); ok {
			fmt.Fprintf(builder, ", Size: %d bytes", int64(size))
		}
	}
	if url := artifactDownloadURL(artifact); url != "" {
		fmt.Fprintf(builder, "\n   Download URL: %s", url)
	}
	if path := artifactLocalPath(artifact); path != "" {
		fmt.Fprintf(builder, "\n   Saved to: %s", path)
	}
	builder.WriteString("\n")
}

// artifactDownloadURL resolves an artifact's download URL: metadata "url"
// first, then the first file part carrying one (agents differ in where
// they put it).
func artifactDownloadURL(artifact adk.Artifact) string {
	if artifact.Metadata != nil {
		if url, ok := (*artifact.Metadata)["url"].(string); ok && url != "" {
			return url
		}
	}
	for _, part := range artifact.Parts {
		if part.URL != nil && *part.URL != "" {
			return *part.URL
		}
	}
	return ""
}

// artifactLocalPath returns the local path recorded by downloadArtifacts, if any.
func artifactLocalPath(artifact adk.Artifact) string {
	if artifact.Metadata == nil {
		return ""
	}
	path, _ := (*artifact.Metadata)["local_path"].(string)
	return path
}

// downloadArtifacts fetches each artifact of a completed task from its
// (trusted agent-host) download URL into the artifacts dir and records the
// local path in the artifact's metadata, so the completion notification can
// point at a file on disk instead of asking the model to WebFetch it.
// Fail-soft: any failure just leaves that artifact with its URL line.
func (t *SubmitTaskTool) downloadArtifacts(ctx context.Context, task *adk.Task) {
	baseDir := t.config.SessionArtifactsDir(agentdomain.GetSessionID(ctx))
	httpClient := &http.Client{Timeout: 60 * time.Second}

	for i := range task.Artifacts {
		artifact := &task.Artifacts[i]
		url := artifactDownloadURL(*artifact)
		if url == "" || !t.config.IsA2AAgentHost(url) {
			continue
		}

		path, err := downloadArtifactFile(httpClient, url, baseDir)
		if err != nil {
			logger.Warn("failed to auto-download A2A artifact", "url", url, "error", err)
			continue
		}

		if artifact.Metadata == nil {
			artifact.Metadata = &adk.Struct{}
		}
		(*artifact.Metadata)["local_path"] = path
		logger.Info("auto-downloaded A2A artifact", "url", url, "path", path)
	}
}

// downloadArtifactFile GETs url into baseDir and returns the absolute path.
func downloadArtifactFile(httpClient *http.Client, url, baseDir string) (string, error) {
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", err
	}
	fullPath := filepath.Join(baseDir, filepath.Base(download.FilenameFromURL(url, resp.Header.Get("Content-Type"))))
	file, err := os.Create(fullPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}

	if absPath, err := filepath.Abs(fullPath); err == nil {
		return absPath, nil
	}
	return fullPath, nil
}

// FormatPreview returns a short preview of the result for UI display
func (t *SubmitTaskTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if result.Data == nil {
		return result.Error
	}

	data, ok := result.Data.(SubmitTaskResult)
	if !ok {
		return "A2A task operation completed"
	}

	preview := fmt.Sprintf("Task %s", data.State)
	if data.Message != "" {
		preview = data.Message
	}

	return preview
}

// FormatForUI formats the result for UI display
func (t *SubmitTaskTool) FormatForUI(result *agentdomain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)

	var output strings.Builder
	fmt.Fprintf(&output, "%s\n", toolCall)
	fmt.Fprintf(&output, "└─ %s %s", statusIcon, preview)

	return output.String()
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *SubmitTaskTool) ShouldCollapseArg(key string) bool {
	return t.formatter.ShouldCollapseArg(key)
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *SubmitTaskTool) ShouldAlwaysExpand() bool {
	return false
}

// errorResult creates an error result
func (t *SubmitTaskTool) errorResult(args map[string]any, startTime time.Time, errorMsg string) (*agentdomain.ToolExecutionResult, error) {
	agentURL, _ := args["agent_url"].(string)

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSubmitTask,
		Arguments: args,
		Success:   false,
		Duration:  time.Since(startTime),
		Error:     errorMsg,
		Data: SubmitTaskResult{
			AgentURL: agentURL,
			State:    string(adk.TaskStateFailed),
			Success:  false,
			Message:  errorMsg,
		},
	}, nil
}

// isTaskNotFoundError checks if the error indicates a task was not found
func (t *SubmitTaskTool) isTaskNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errorStr := strings.ToLower(err.Error())
	return strings.Contains(errorStr, "task not found") ||
		strings.Contains(errorStr, "not found") ||
		strings.Contains(errorStr, "32603")
}

// mapToStruct converts a map[string]any to a struct using JSON marshaling
func mapToStruct(data any, target any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}
