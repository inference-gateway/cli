package scheduler_test

import (
	"context"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"

	schedmocks "github.com/inference-gateway/cli/tests/mocks/scheduler"

	config "github.com/inference-gateway/cli/config"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	scheduler "github.com/inference-gateway/cli/internal/scheduler"
	scheddomain "github.com/inference-gateway/cli/internal/scheduler/domain"
)

var _ scheddomain.TitleGenerator = (*schedmocks.FakeTitleGenerator)(nil)

func TestBackgroundJobManager_StartStop(t *testing.T) {
	mockTitleGenerator := &schedmocks.FakeTitleGenerator{}

	manager := scheduler.NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

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
	mockTitleGenerator := &schedmocks.FakeTitleGenerator{}
	mockTitleGenerator.ProcessPendingTitlesReturns(nil)

	manager := scheduler.NewBackgroundJobManager(mockTitleGenerator, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}

func TestBackgroundJobManager_TriggerTitleGeneration_NilGenerator(t *testing.T) {
	manager := scheduler.NewBackgroundJobManager(nil, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}
