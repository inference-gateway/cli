package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	adk "github.com/inference-gateway/adk/types"
	config "github.com/inference-gateway/cli/config"
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
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			require.NotNil(t, service)
			assert.Equal(t, tt.config, service.config)
		})
	}
}

func TestA2ADirectService_SubmitTask(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.Config
		agentURL string
		task     adk.Task
		wantErr  bool
		errMsg   string
	}{
		{
			name: "disabled A2A returns error",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			agentURL: "http://test-agent.example.com",
			task: adk.Task{
				ID:   uuid.New().String(),
				Kind: "test",
			},
			wantErr: true,
			errMsg:  "A2A direct connections are disabled",
		},
		{
			name: "successful task submission",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			},
			agentURL: "http://test-agent.example.com",
			task: adk.Task{
				ID:   uuid.New().String(),
				Kind: "test",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			ctx := context.Background()

			result, err := service.SubmitTask(ctx, tt.agentURL, tt.task)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.task.ID, result.ID)
				assert.Equal(t, adk.TaskStateSubmitted, result.Status.State)
			}
		})
	}
}

func TestA2ADirectService_Query(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.Config
		agentURL string
		wantErr  bool
		errMsg   string
	}{
		{
			name: "disabled A2A returns error",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: false,
				},
			},
			agentURL: "http://test-agent.example.com",
			wantErr:  true,
			errMsg:   "A2A direct connections are disabled",
		},
		{
			name: "successful query would contact agent",
			config: &config.Config{
				A2A: config.A2AConfig{
					Enabled: true,
				},
			},
			agentURL: "http://test-agent.example.com",
			wantErr:  true,
			errMsg:   "failed to query agent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewA2ADirectService(tt.config)
			ctx := context.Background()

			result, err := service.Query(ctx, tt.agentURL)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}
