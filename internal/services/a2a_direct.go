package services

import (
	"context"
	"fmt"

	adkClient "github.com/inference-gateway/adk/client"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// A2ADirectServiceImpl implements the A2ADirectService interface
type A2ADirectServiceImpl struct {
	config *config.Config
}

// NewA2ADirectService creates a new A2A direct service
func NewA2ADirectService(cfg *config.Config) *A2ADirectServiceImpl {
	return &A2ADirectServiceImpl{
		config: cfg,
	}
}

// SubmitTask submits a task to a specific A2A agent
func (s *A2ADirectServiceImpl) SubmitTask(ctx context.Context, agentURL string, task adk.Task) (*adk.Task, error) {
	if !s.config.IsA2ADirectEnabled() {
		return nil, fmt.Errorf("A2A direct connections are disabled")
	}

	task.Status = adk.TaskStatus{
		State: adk.TaskStateSubmitted,
	}

	logger.Debug("Task submitted to A2A agent", "task_id", task.ID, "agent_url", agentURL)
	return &task, nil
}

// Query sends a query to a specific A2A agent
func (s *A2ADirectServiceImpl) Query(ctx context.Context, agentURL string) (*adk.AgentCard, error) {
	if !s.config.IsA2ADirectEnabled() {
		return nil, fmt.Errorf("A2A direct connections are disabled")
	}

	client := adkClient.NewClient(agentURL)
	response, err := client.GetAgentCard(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent at %s: %w", agentURL, err)
	}

	logger.Debug("Query sent to A2A agent", "agent_url", agentURL)
	return response, nil
}
