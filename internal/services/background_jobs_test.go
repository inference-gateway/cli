package services_test

import (
	"context"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/constants"
	services "github.com/inference-gateway/cli/internal/services"
	servicesmocks "github.com/inference-gateway/cli/tests/mocks/services"
	assert "github.com/stretchr/testify/assert"
)

var _ services.TitleGenerator = (*servicesmocks.FakeTitleGenerator)(nil)

func TestBackgroundJobManager_StartStop(t *testing.T) {
	mockTitleGenerator := &servicesmocks.FakeTitleGenerator{}

	manager := services.NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

	assert.False(t, manager.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	assert.True(t, manager.IsRunning())

	time.Sleep(constants.TestSleepDelay)

	manager.Stop()
	assert.False(t, manager.IsRunning())
}

func TestBackgroundJobManager_TriggerTitleGeneration(t *testing.T) {
	mockTitleGenerator := &servicesmocks.FakeTitleGenerator{}
	mockTitleGenerator.ProcessPendingTitlesReturns(nil)

	manager := services.NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}

func TestBackgroundJobManager_TriggerTitleGeneration_NilGenerator(t *testing.T) {
	manager := services.NewBackgroundJobManager(nil, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}
