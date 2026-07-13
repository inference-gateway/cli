package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	client "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// a2aJobController is the narrow job-supervisor surface this service needs: the
// live A2A polling states (the single source for active A2A rows, shared with the
// status-bar indicator) and the wind control used to stop a task on cancel.
// *jobs.Supervisor satisfies it.
type a2aJobController interface {
	A2APollingStates() []domain.TaskPollingState
	Wind(id string, sig domain.WindSignal) error
}

// BackgroundTaskService handles background task operations (A2A-specific)
// Only instantiated when A2A tools are enabled
type BackgroundTaskService struct {
	taskTracker     domain.A2ATaskTracker
	jobs            a2aJobController
	createADKClient func(agentURL string) client.A2AClient
	mutex           sync.RWMutex
}

// NewBackgroundTaskService creates a new background task service. jobs is the job
// supervisor - the single source of truth for which A2A tasks are running - while
// taskTracker still resolves the context graph and a task's agent URL for cancel.
func NewBackgroundTaskService(taskTracker domain.A2ATaskTracker, jobs a2aJobController) *BackgroundTaskService {
	return &BackgroundTaskService{
		taskTracker: taskTracker,
		jobs:        jobs,
		createADKClient: func(agentURL string) client.A2AClient {
			return telemetry.NewA2AClient(agentURL)
		},
	}
}

// GetBackgroundTasks returns the active A2A tasks from the job supervisor - the
// single source of truth shared with the status-bar indicator - so the /tasks
// active list and the indicator can no longer diverge.
func (s *BackgroundTaskService) GetBackgroundTasks() []domain.TaskPollingState {
	if s.jobs == nil {
		return []domain.TaskPollingState{}
	}
	return s.jobs.A2APollingStates()
}

// CancelBackgroundTask cancels a background task by task ID
func (s *BackgroundTaskService) CancelBackgroundTask(taskID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger.Info("canceling background task", "task_id", taskID)

	if s.taskTracker == nil {
		return fmt.Errorf("task tracker not available")
	}

	targetTask := s.taskTracker.GetPollingState(taskID)
	if targetTask == nil {
		return fmt.Errorf("task %s not found in background tasks", taskID)
	}

	if err := s.sendCancelToAgent(targetTask); err != nil {
		logger.Error("failed to send cancel to agent", "task_id", taskID, "error", err)
	} else {
		logger.Info("successfully sent cancel request to agent", "task_id", taskID)
	}

	if s.jobs != nil {
		if err := s.jobs.Wind(taskID, domain.WindStop); err != nil {
			logger.Warn("failed to wind supervised A2A job on cancel", "task_id", taskID, "error", err)
		}
	}

	s.taskTracker.StopPolling(taskID)
	s.taskTracker.RemoveTask(taskID)

	logger.Info("task cancelled successfully", "task_id", taskID)
	return nil
}

// sendCancelToAgent sends a cancel request to the agent server
func (s *BackgroundTaskService) sendCancelToAgent(task *domain.TaskPollingState) error {
	adkClient := s.createADKClient(task.AgentURL)

	taskStatus, err := adkClient.GetTask(context.Background(), adk.TaskQueryParams{
		ID: task.TaskID,
	})

	if err != nil {
		logger.Warn("failed to query task status before canceling, proceeding with cancel anyway", "task_id", task.TaskID, "error", err)
	} else if taskStatus != nil {
		var currentTask adk.Task
		if mapErr := mapToStruct(taskStatus.Result, &currentTask); mapErr == nil {
			switch currentTask.Status.State {
			case adk.TaskStateCompleted, adk.TaskStateFailed, adk.TaskStateCancelled, adk.TaskStateRejected:
				logger.Info("task is already in terminal state, skipping cancel request",
					"task_id", task.TaskID,
					"state", currentTask.Status.State)
				return nil
			}
		}
	}

	_, err = adkClient.CancelTask(context.Background(), adk.TaskIdParams{
		ID: task.TaskID,
	})

	if err != nil {
		logger.Error("aDK CancelTask returned error", "task_id", task.TaskID, "error", err)
		return err
	}

	return nil
}

// mapToStruct converts a map[string]any to a struct using JSON marshaling
func mapToStruct(data any, target any) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, target)
}
