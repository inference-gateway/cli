package scheduler_test

import (
	"context"
	"testing"
	"testing/synctest"
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

	manager := scheduler.NewTitleBackfill(mockTitleGenerator, &config.Config{})

	assert.False(t, manager.IsRunning())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	manager.Start(ctx)
	assert.True(t, manager.IsRunning())

	time.Sleep(constants.TestSleepDelay)

	manager.Stop()
	assert.False(t, manager.IsRunning())
}

func TestBackgroundJobManager_StopCancelsInFlightPassAndRestarts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		generator := &schedmocks.FakeTitleGenerator{}
		started := make(chan struct{}, 1)
		generator.ProcessPendingTitlesStub = func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}
		cfg := &config.Config{}
		cfg.Conversation.TitleGeneration.Interval = 1
		manager := scheduler.NewTitleBackfill(generator, cfg)

		manager.Start(t.Context())
		<-started
		stopAt := time.Now()
		manager.Stop()
		assert.Less(t, time.Since(stopAt), 10*time.Second, "Stop must cancel the in-flight pass, not wait out its timeout")
		assert.False(t, manager.IsRunning())

		manager.Start(t.Context())
		assert.True(t, manager.IsRunning())
		manager.Stop()
	})
}

func TestBackgroundJobManager_TriggerTitleGeneration(t *testing.T) {
	mockTitleGenerator := &schedmocks.FakeTitleGenerator{}
	mockTitleGenerator.ProcessPendingTitlesReturns(nil)

	manager := scheduler.NewTitleBackfill(mockTitleGenerator, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}

func TestBackgroundJobManager_TriggerTitleGeneration_NilGenerator(t *testing.T) {
	manager := scheduler.NewTitleBackfill(nil, &config.Config{})

	err := manager.TriggerTitleGeneration(context.Background())
	assert.NoError(t, err)
}
