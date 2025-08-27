package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestBackgroundJobManager_StartStop(t *testing.T) {
	mockTitleGenerator := &MockTitleGenerator{}

	manager := NewBackgroundJobManager(mockTitleGenerator)

	assert.False(t, manager.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	assert.True(t, manager.IsRunning())

	time.Sleep(100 * time.Millisecond)

	manager.Stop()
	assert.False(t, manager.IsRunning())

	mockTitleGenerator.AssertExpectations(t)
}

func TestBackgroundJobManager_TriggerTitleGeneration(t *testing.T) {
	mockTitleGenerator := &MockTitleGenerator{}
	mockTitleGenerator.On("ProcessPendingTitles", mock.Anything).Return(nil)

	manager := NewBackgroundJobManager(mockTitleGenerator)

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)

	mockTitleGenerator.AssertExpectations(t)
}

func TestBackgroundJobManager_TriggerTitleGeneration_NilGenerator(t *testing.T) {
	manager := NewBackgroundJobManager(nil)

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}

type MockTitleGenerator struct {
	mock.Mock
}

func (m *MockTitleGenerator) ProcessPendingTitles(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
