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
)

// BackgroundTaskService handles background task operations (A2A-specific)
// Only instantiated when A2A tools are enabled
type BackgroundTaskService struct {
	taskTracker     domain.TaskTracker
	createADKClient func(agentURL string) client.A2AClient
	mutex           sync.RWMutex
}

// NewBackgroundTaskService creates a new background task service
func NewBackgroundTaskService(taskTracker domain.TaskTracker) *BackgroundTaskService {
	return &BackgroundTaskService{
		taskTracker: taskTracker,
		createADKClient: func(agentURL string) client.A2AClient {
			return client.NewClient(agentURL)
		},
	}
}

// GetBackgroundTasks returns all current background polling tasks
func (s *BackgroundTaskService) GetBackgroundTasks() []domain.TaskPollingState {
	if s.taskTracker == nil {
		return []domain.TaskPollingState{}
	}

	pollingTasks := s.taskTracker.GetAllPollingTasks()
	tasks := make([]domain.TaskPollingState, 0, len(pollingTasks))

	for _, taskID := range pollingTasks {
		if state := s.taskTracker.GetPollingState(taskID); state != nil {
			tasks = append(tasks, *state)
		}
	}

	return tasks
}

// CancelBackgroundTask cancels a background task by task ID
func (s *BackgroundTaskService) CancelBackgroundTask(taskID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger.Info("Canceling background task", "task_id", taskID)

	backgroundTasks := s.GetBackgroundTasks()

	var targetTask *domain.TaskPollingState
	for i := range backgroundTasks {
		if backgroundTasks[i].TaskID == taskID {
			targetTask = &backgroundTasks[i]
			break
		}
	}

	if targetTask == nil {
		return fmt.Errorf("task %s not found in background tasks", taskID)
	}

	if err := s.sendCancelToAgent(targetTask); err != nil {
		logger.Error("Failed to send cancel to agent", "task_id", taskID, "error", err)
	} else {
		logger.Info("Successfully sent cancel request to agent", "task_id", taskID)
	}

	if targetTask.CancelFunc != nil {
		targetTask.CancelFunc()
	}

	if s.taskTracker != nil {
		s.taskTracker.StopPolling(taskID)
		s.taskTracker.RemoveTask(taskID)
	}

	logger.Info("Task cancelled successfully", "task_id", taskID)
	return nil
}

// sendCancelToAgent sends a cancel request to the agent server
func (s *BackgroundTaskService) sendCancelToAgent(task *domain.TaskPollingState) error {
	adkClient := s.createADKClient(task.AgentURL)

	taskStatus, err := adkClient.GetTask(context.Background(), adk.TaskQueryParams{
		ID: task.TaskID,
	})

	if err != nil {
		logger.Warn("Failed to query task status before canceling, proceeding with cancel anyway", "task_id", task.TaskID, "error", err)
	} else if taskStatus != nil {
		var currentTask adk.Task
		if mapErr := mapToStruct(taskStatus.Result, &currentTask); mapErr == nil {
			switch currentTask.Status.State {
			case adk.TaskStateCompleted, adk.TaskStateFailed, adk.TaskStateCanceled, adk.TaskStateRejected:
				logger.Info("Task is already in terminal state, skipping cancel request",
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
		logger.Error("ADK CancelTask returned error", "task_id", task.TaskID, "error", err)
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
