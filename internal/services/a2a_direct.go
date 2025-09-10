package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/google/uuid"
	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/inference-gateway/cli/internal/logger"
)

// A2ADirectServiceImpl implements the A2ADirectService interface
type A2ADirectServiceImpl struct {
	config           *config.Config
	client           *resty.Client
	activeTasks      map[string]*A2ATaskTracker
	activeTasksMux   sync.RWMutex
	backgroundJobs   sync.WaitGroup
	shutdownChan     chan struct{}
	statusPollers    map[string]context.CancelFunc
	statusPollersMux sync.RWMutex
}

// A2ATaskTracker tracks the state of an active A2A task
type A2ATaskTracker struct {
	TaskID    string
	AgentName string
	Task      domain.A2ATask
	Status    *domain.A2ATaskStatus
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewA2ADirectService creates a new A2A direct service
func NewA2ADirectService(cfg *config.Config) *A2ADirectServiceImpl {
	client := resty.New().
		SetTimeout(30 * time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(time.Second).
		SetRetryMaxWaitTime(5 * time.Second)

	return &A2ADirectServiceImpl{
		config:        cfg,
		client:        client,
		activeTasks:   make(map[string]*A2ATaskTracker),
		shutdownChan:  make(chan struct{}),
		statusPollers: make(map[string]context.CancelFunc),
	}
}

// SubmitTask submits a task to a specific A2A agent in the background
func (s *A2ADirectServiceImpl) SubmitTask(ctx context.Context, agentName string, task domain.A2ATask) (string, error) {
	if !s.config.IsA2ADirectEnabled() {
		return "", fmt.Errorf("A2A direct connections are disabled")
	}

	agent, exists := s.config.GetA2AAgent(agentName)
	if !exists {
		return "", fmt.Errorf("agent '%s' not found in configuration", agentName)
	}

	if !agent.Enabled {
		return "", fmt.Errorf("agent '%s' is disabled", agentName)
	}

	if task.ID == "" {
		task.ID = uuid.New().String()
	}

	taskTracker := &A2ATaskTracker{
		TaskID:    task.ID,
		AgentName: agentName,
		Task:      task,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Status: &domain.A2ATaskStatus{
			TaskID:    task.ID,
			Status:    domain.A2ATaskStatusPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	s.activeTasksMux.Lock()
	s.activeTasks[task.ID] = taskTracker
	s.activeTasksMux.Unlock()

	s.backgroundJobs.Add(1)
	go s.submitTaskAsync(ctx, agent, task, taskTracker)

	logger.Debug("A2A task submitted", "task_id", task.ID, "agent", agentName)
	return task.ID, nil
}

// submitTaskAsync handles the asynchronous task submission
func (s *A2ADirectServiceImpl) submitTaskAsync(ctx context.Context, agent config.A2AAgentInfo, task domain.A2ATask, tracker *A2ATaskTracker) {
	defer s.backgroundJobs.Done()

	s.updateTaskStatus(tracker.TaskID, "submitting", 0, "Submitting task to agent")

	endpoint := fmt.Sprintf("%s/api/v1/tasks", strings.TrimSuffix(agent.URL, "/"))

	payload := map[string]interface{}{
		"id":          task.ID,
		"type":        task.Type,
		"description": task.Description,
		"parameters":  task.Parameters,
		"priority":    task.Priority,
		"timeout":     task.Timeout,
	}

	request := s.client.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(payload)

	if agent.APIKey != "" {
		request.SetAuthToken(agent.APIKey)
	}

	resp, err := request.Post(endpoint)
	if err != nil {
		s.updateTaskStatus(tracker.TaskID, domain.A2ATaskStatusFailed, 0, fmt.Sprintf("Failed to submit task: %v", err))
		logger.Error("Failed to submit A2A task", "task_id", task.ID, "agent", agent.Name, "error", err)
		return
	}

	if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusAccepted {
		s.updateTaskStatus(tracker.TaskID, domain.A2ATaskStatusFailed, 0, fmt.Sprintf("Task submission failed: %s", resp.String()))
		logger.Error("A2A task submission failed", "task_id", task.ID, "status", resp.Status(), "response", resp.String())
		return
	}

	var submitResponse struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}

	if err := json.Unmarshal(resp.Body(), &submitResponse); err != nil {
		s.updateTaskStatus(tracker.TaskID, domain.A2ATaskStatusFailed, 0, fmt.Sprintf("Failed to parse submission response: %v", err))
		logger.Error("Failed to parse A2A task submission response", "task_id", task.ID, "error", err)
		return
	}

	s.updateTaskStatus(tracker.TaskID, domain.A2ATaskStatusRunning, 10, "Task submitted successfully")

	// Start status polling
	s.startStatusPolling(ctx, agent, tracker.TaskID)

	logger.Debug("A2A task submitted successfully", "task_id", task.ID, "agent", agent.Name)
}

// startStatusPolling starts a goroutine to poll task status
func (s *A2ADirectServiceImpl) startStatusPolling(ctx context.Context, agent config.A2AAgentInfo, taskID string) {
	pollCtx, cancel := context.WithCancel(ctx)

	s.statusPollersMux.Lock()
	s.statusPollers[taskID] = cancel
	s.statusPollersMux.Unlock()

	s.backgroundJobs.Add(1)
	go s.pollTaskStatus(pollCtx, agent, taskID)
}

// pollTaskStatus polls the task status periodically
func (s *A2ADirectServiceImpl) pollTaskStatus(ctx context.Context, agent config.A2AAgentInfo, taskID string) {
	defer s.backgroundJobs.Done()
	defer func() {
		s.statusPollersMux.Lock()
		delete(s.statusPollers, taskID)
		s.statusPollersMux.Unlock()
	}()

	pollInterval := time.Duration(s.config.GetA2ATaskConfig().StatusPollSeconds) * time.Second
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.shutdownChan:
			return
		case <-ticker.C:
			status, err := s.fetchTaskStatus(ctx, agent, taskID)
			if err != nil {
				logger.Error("Failed to fetch A2A task status", "task_id", taskID, "error", err)
				continue
			}

			s.activeTasksMux.Lock()
			if tracker, exists := s.activeTasks[taskID]; exists {
				tracker.Status = status
				tracker.UpdatedAt = time.Now()
			}
			s.activeTasksMux.Unlock()

			// Stop polling if task is completed
			if status.Status == domain.A2ATaskStatusCompleted || status.Status == domain.A2ATaskStatusFailed {
				return
			}
		}
	}
}

// fetchTaskStatus fetches the current status of a task from the agent
func (s *A2ADirectServiceImpl) fetchTaskStatus(ctx context.Context, agent config.A2AAgentInfo, taskID string) (*domain.A2ATaskStatus, error) {
	endpoint := fmt.Sprintf("%s/api/v1/tasks/%s/status", strings.TrimSuffix(agent.URL, "/"), taskID)

	request := s.client.R().
		SetContext(ctx)

	if agent.APIKey != "" {
		request.SetAuthToken(agent.APIKey)
	}

	resp, err := request.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch task status: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch task status: %s", resp.String())
	}

	var status domain.A2ATaskStatus
	if err := json.Unmarshal(resp.Body(), &status); err != nil {
		return nil, fmt.Errorf("failed to parse status response: %w", err)
	}

	return &status, nil
}

// GetTaskStatus retrieves the current status of a task
func (s *A2ADirectServiceImpl) GetTaskStatus(ctx context.Context, taskID string) (*domain.A2ATaskStatus, error) {
	s.activeTasksMux.RLock()
	tracker, exists := s.activeTasks[taskID]
	s.activeTasksMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("task '%s' not found", taskID)
	}

	return tracker.Status, nil
}

// CollectResults collects completed task results
func (s *A2ADirectServiceImpl) CollectResults(ctx context.Context, taskID string) (*domain.A2ATaskResult, error) {
	s.activeTasksMux.RLock()
	tracker, exists := s.activeTasks[taskID]
	s.activeTasksMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("task '%s' not found", taskID)
	}

	agent, exists := s.config.GetA2AAgent(tracker.AgentName)
	if !exists {
		return nil, fmt.Errorf("agent '%s' not found", tracker.AgentName)
	}

	endpoint := fmt.Sprintf("%s/api/v1/tasks/%s/result", strings.TrimSuffix(agent.URL, "/"), taskID)

	request := s.client.R().
		SetContext(ctx)

	if agent.APIKey != "" {
		request.SetAuthToken(agent.APIKey)
	}

	resp, err := request.Get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch task result: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch task result: %s", resp.String())
	}

	var result domain.A2ATaskResult
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("failed to parse result response: %w", err)
	}

	// Clean up completed task
	s.activeTasksMux.Lock()
	delete(s.activeTasks, taskID)
	s.activeTasksMux.Unlock()

	return &result, nil
}

// CancelTask cancels a running task
func (s *A2ADirectServiceImpl) CancelTask(ctx context.Context, taskID string) error {
	s.activeTasksMux.RLock()
	tracker, exists := s.activeTasks[taskID]
	s.activeTasksMux.RUnlock()

	if !exists {
		return fmt.Errorf("task '%s' not found", taskID)
	}

	agent, exists := s.config.GetA2AAgent(tracker.AgentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found", tracker.AgentName)
	}

	// Stop status polling
	s.statusPollersMux.Lock()
	if cancel, exists := s.statusPollers[taskID]; exists {
		cancel()
		delete(s.statusPollers, taskID)
	}
	s.statusPollersMux.Unlock()

	endpoint := fmt.Sprintf("%s/api/v1/tasks/%s/cancel", strings.TrimSuffix(agent.URL, "/"), taskID)

	request := s.client.R().
		SetContext(ctx)

	if agent.APIKey != "" {
		request.SetAuthToken(agent.APIKey)
	}

	resp, err := request.Post(endpoint)
	if err != nil {
		return fmt.Errorf("failed to cancel task: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to cancel task: %s", resp.String())
	}

	s.updateTaskStatus(taskID, "cancelled", 0, "Task cancelled by user")
	logger.Debug("A2A task cancelled", "task_id", taskID)

	return nil
}

// ListActiveAgents returns all currently active A2A agents
func (s *A2ADirectServiceImpl) ListActiveAgents() (map[string]domain.A2AAgentInfo, error) {
	configAgents := s.config.GetEnabledA2AAgents()
	activeAgents := make(map[string]domain.A2AAgentInfo)

	for name, agent := range configAgents {
		domainAgent := domain.A2AAgentInfo{
			Name:        agent.Name,
			URL:         agent.URL,
			APIKey:      agent.APIKey,
			Description: agent.Description,
			Timeout:     agent.Timeout,
			Enabled:     agent.Enabled,
			Metadata:    agent.Metadata,
		}
		activeAgents[name] = domainAgent
	}

	return activeAgents, nil
}

// TestConnection tests connectivity to a specific agent
func (s *A2ADirectServiceImpl) TestConnection(ctx context.Context, agentName string) error {
	agent, exists := s.config.GetA2AAgent(agentName)
	if !exists {
		return fmt.Errorf("agent '%s' not found in configuration", agentName)
	}

	endpoint := fmt.Sprintf("%s/api/v1/health", strings.TrimSuffix(agent.URL, "/"))

	timeoutCtx, cancel := context.WithTimeout(ctx, time.Duration(agent.Timeout)*time.Second)
	defer cancel()

	request := s.client.R().
		SetContext(timeoutCtx)

	if agent.APIKey != "" {
		request.SetAuthToken(agent.APIKey)
	}

	resp, err := request.Get(endpoint)
	if err != nil {
		return fmt.Errorf("connection to agent '%s' failed: %w", agentName, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return fmt.Errorf("agent '%s' health check failed: %s", agentName, resp.Status())
	}

	logger.Debug("A2A agent connection test successful", "agent", agentName)
	return nil
}

// updateTaskStatus updates the status of a tracked task
func (s *A2ADirectServiceImpl) updateTaskStatus(taskID string, status domain.A2ATaskStatusEnum, progress float64, message string) {
	s.activeTasksMux.Lock()
	defer s.activeTasksMux.Unlock()

	if tracker, exists := s.activeTasks[taskID]; exists {
		tracker.Status.Status = status
		tracker.Status.Progress = progress
		tracker.Status.Message = message
		tracker.Status.UpdatedAt = time.Now()
		tracker.UpdatedAt = time.Now()

		if status == domain.A2ATaskStatusCompleted || status == domain.A2ATaskStatusFailed {
			now := time.Now()
			tracker.Status.CompletedAt = &now
		}
	}
}

// Shutdown gracefully shuts down the A2A direct service
func (s *A2ADirectServiceImpl) Shutdown(ctx context.Context) error {
	close(s.shutdownChan)

	// Cancel all status pollers
	s.statusPollersMux.Lock()
	for _, cancel := range s.statusPollers {
		cancel()
	}
	s.statusPollersMux.Unlock()

	// Wait for background jobs to finish with timeout
	done := make(chan struct{})
	go func() {
		s.backgroundJobs.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Debug("A2A direct service shutdown completed")
		return nil
	case <-ctx.Done():
		logger.Error("A2A direct service shutdown timeout")
		return ctx.Err()
	}
}
