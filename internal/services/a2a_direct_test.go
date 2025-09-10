package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewA2ADirectService(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
	}{
		{
			name: "creates service with default config",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
					Tasks: config.A2ATaskConfig{
						MaxConcurrent:     3,
						TimeoutSeconds:    300,
						RetryCount:        2,
						StatusPollSeconds: 5,
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			require.NotNil(t, service)
			assert.Equal(t, tt.config, service.config)
			assert.NotNil(t, service.client)
			assert.NotNil(t, service.activeTasks)
			assert.NotNil(t, service.shutdownChan)
			assert.NotNil(t, service.statusPollers)
		})
	}
}

func TestA2ADirectServiceImpl_SubmitTask(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.Config
		agentName string
		task      domain.A2ATask
		wantErr   bool
		errMsg    string
	}{
		{
			name: "fails when A2A direct connections disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			agentName: "test-agent",
			task: domain.A2ATask{
				Task: adk.Task{
					ID:   uuid.New().String(),
					Kind: "test",
					Metadata: map[string]any{
						"description": "Test task",
					},
				},
				JobType: "test",
			},
			wantErr: true,
			errMsg:  "A2A direct connections are disabled",
		},
		{
			name: "fails when agent not found",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			},
			agentName: "nonexistent-agent",
			task: domain.A2ATask{
				Task: adk.Task{
					ID:   uuid.New().String(),
					Kind: "test",
					Metadata: map[string]any{
						"description": "Test task",
					},
				},
				JobType: "test",
			},
			wantErr: true,
			errMsg:  "agent 'nonexistent-agent' not found in configuration",
		},
		{
			name: "fails when agent disabled",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents: map[string]config.A2AAgentInfo{
						"test-agent": {
							Name:    "test-agent",
							URL:     "http://localhost:8081",
							Enabled: false,
						},
					},
				},
			},
			agentName: "test-agent",
			task: domain.A2ATask{
				Task: adk.Task{
					ID:   uuid.New().String(),
					Kind: "test",
					Metadata: map[string]any{
						"description": "Test task",
					},
				},
				JobType: "test",
			},
			wantErr: true,
			errMsg:  "agent 'test-agent' is disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			ctx := context.Background()

			taskID, err := service.SubmitTask(ctx, tt.agentName, tt.task)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Empty(t, taskID)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, taskID)
			}
		})
	}
}

func TestA2ADirectServiceImpl_GetTaskStatus(t *testing.T) {
	tests := []struct {
		name    string
		taskID  string
		setup   func(*A2ADirectServiceImpl)
		wantErr bool
		errMsg  string
	}{
		{
			name:    "fails for nonexistent task",
			taskID:  "nonexistent-task",
			setup:   func(s *A2ADirectServiceImpl) {},
			wantErr: true,
			errMsg:  "task 'nonexistent-task' not found",
		},
		{
			name:   "returns status for existing task",
			taskID: "test-task",
			setup: func(s *A2ADirectServiceImpl) {
				s.activeTasks["test-task"] = &A2ATaskTracker{
					TaskID:    "test-task",
					AgentName: "test-agent",
					Status: &domain.A2ATaskStatus{
						TaskID: "test-task",
						TaskStatus: adk.TaskStatus{
							State: domain.A2ATaskStatusWorking,
							Message: adk.NewStreamingStatusMessage(
								uuid.New().String(),
								"Task in progress",
								map[string]any{"progress": 50.0},
							),
						},
						Progress:  50.0,
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					},
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			}
			service := NewA2ADirectService(cfg)
			tt.setup(service)
			ctx := context.Background()

			status, err := service.GetTaskStatus(ctx, tt.taskID)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, status)
				assert.Equal(t, tt.taskID, status.TaskID)
			}
		})
	}
}

func TestA2ADirectServiceImpl_ListActiveAgents(t *testing.T) {
	tests := []struct {
		name   string
		config *config.Config
		want   int
	}{
		{
			name: "returns empty map when no agents configured",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			},
			want: 0,
		},
		{
			name: "returns only enabled agents",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents: map[string]config.A2AAgentInfo{
						"agent1": {
							Name:    "agent1",
							URL:     "http://localhost:8081",
							Enabled: true,
						},
						"agent2": {
							Name:    "agent2",
							URL:     "http://localhost:8082",
							Enabled: false,
						},
						"agent3": {
							Name:    "agent3",
							URL:     "http://localhost:8083",
							Enabled: true,
						},
					},
				},
			},
			want: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)

			agents, err := service.ListActiveAgents()

			require.NoError(t, err)
			assert.Len(t, agents, tt.want)

			// Verify all returned agents are enabled
			for _, agent := range agents {
				assert.True(t, agent.Enabled)
			}
		})
	}
}

func TestA2ADirectServiceImpl_TestConnection(t *testing.T) {
	tests := []struct {
		name      string
		config    *config.Config
		agentName string
		wantErr   bool
		errMsg    string
	}{
		{
			name: "fails when agent not found",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			},
			agentName: "nonexistent-agent",
			wantErr:   true,
			errMsg:    "agent 'nonexistent-agent' not found in configuration",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			ctx := context.Background()

			err := service.TestConnection(ctx, tt.agentName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestA2ADirectServiceImpl_updateTaskStatus(t *testing.T) {
	tests := []struct {
		name     string
		taskID   string
		status   domain.A2ATaskStatusEnum
		progress float64
		message  string
		setup    func(*A2ADirectServiceImpl)
		verify   func(*testing.T, *A2ADirectServiceImpl)
	}{
		{
			name:     "updates existing task status",
			taskID:   "test-task",
			status:   domain.A2ATaskStatusCompleted,
			progress: 100.0,
			message:  "Task completed successfully",
			setup: func(s *A2ADirectServiceImpl) {
				s.activeTasks["test-task"] = &A2ATaskTracker{
					TaskID:    "test-task",
					AgentName: "test-agent",
					Status: &domain.A2ATaskStatus{
						TaskID: "test-task",
						TaskStatus: adk.TaskStatus{
							State: domain.A2ATaskStatusWorking,
							Message: adk.NewStreamingStatusMessage(
								uuid.New().String(),
								"Task in progress",
								map[string]any{"progress": 50.0},
							),
						},
						Progress:  50.0,
						CreatedAt: time.Now(),
						UpdatedAt: time.Now(),
					},
				}
			},
			verify: func(t *testing.T, s *A2ADirectServiceImpl) {
				task, exists := s.activeTasks["test-task"]
				require.True(t, exists)
				assert.Equal(t, domain.A2ATaskStatusCompleted, task.Status.Status())
				assert.Equal(t, 100.0, task.Status.Progress)
				// Check message is in ADK TaskStatus.Message
				require.NotNil(t, task.Status.TaskStatus.Message)
				assert.NotNil(t, task.Status.CompletedAt)
			},
		},
		{
			name:     "ignores update for nonexistent task",
			taskID:   "nonexistent-task",
			status:   domain.A2ATaskStatusCompleted,
			progress: 100.0,
			message:  "Task completed",
			setup:    func(s *A2ADirectServiceImpl) {},
			verify: func(t *testing.T, s *A2ADirectServiceImpl) {
				_, exists := s.activeTasks["nonexistent-task"]
				assert.False(t, exists)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			}
			service := NewA2ADirectService(cfg)
			tt.setup(service)

			service.updateTaskStatus(tt.taskID, tt.status, tt.progress, tt.message)

			tt.verify(t, service)
		})
	}
}

func TestA2ADirectServiceImpl_Shutdown(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		wantErr bool
	}{
		{
			name:    "shuts down successfully with adequate timeout",
			timeout: 5 * time.Second,
			wantErr: false,
		},
		{
			name:    "times out with insufficient timeout",
			timeout: 1 * time.Nanosecond,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
					Agents:  make(map[string]config.A2AAgentInfo),
				},
			}
			service := NewA2ADirectService(cfg)

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			err := service.Shutdown(ctx)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
