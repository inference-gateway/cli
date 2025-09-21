package services

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/constants"
	assert "github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
)

func TestBackgroundJobManager_StartStop(t *testing.T) {
	mockTitleGenerator := &MockTitleGenerator{}

	manager := NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

	assert.False(t, manager.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	assert.True(t, manager.IsRunning())

	time.Sleep(constants.TestSleepDelay)

	manager.Stop()
	assert.False(t, manager.IsRunning())

	mockTitleGenerator.AssertExpectations(t)
}

func TestBackgroundJobManager_TriggerTitleGeneration(t *testing.T) {
	mockTitleGenerator := &MockTitleGenerator{}
	mockTitleGenerator.On("ProcessPendingTitles", mock.Anything).Return(nil)

	manager := NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)

	mockTitleGenerator.AssertExpectations(t)
}

func TestBackgroundJobManager_TriggerTitleGeneration_NilGenerator(t *testing.T) {
	manager := NewBackgroundJobManager(nil, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}

func TestBackgroundJobManager_GetJobInterval(t *testing.T) {
	testCases := []struct {
		name             string
		config           *config.Config
		expectedInterval time.Duration
	}{
		{
			name:             "Default interval when config is nil",
			config:           nil,
			expectedInterval: 5 * time.Minute,
		},
		{
			name:             "Default interval when interval is 0",
			config:           &config.Config{},
			expectedInterval: 5 * time.Minute,
		},
		{
			name: "Custom interval from config",
			config: &config.Config{
				Conversation: config.ConversationConfig{
					TitleGeneration: config.ConversationTitleConfig{
						Interval: 120,
					},
				},
			},
			expectedInterval: 2 * time.Minute,
		},
		{
			name: "Very short interval from config",
			config: &config.Config{
				Conversation: config.ConversationConfig{
					TitleGeneration: config.ConversationTitleConfig{
						Interval: 30,
					},
				},
			},
			expectedInterval: 30 * time.Second,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			manager := NewBackgroundJobManager(nil, tc.config)
			interval := manager.getJobInterval()
			assert.Equal(t, tc.expectedInterval, interval)
		})
	}
}

type MockTitleGenerator struct {
	mock.Mock
}

func (m *MockTitleGenerator) ProcessPendingTitles(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
