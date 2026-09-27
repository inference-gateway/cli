package infrastructure

import (
	"errors"
	"testing"

	assert "github.com/stretchr/testify/assert"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

func TestLogCaptureResultWarnsOncePerFailureStreak(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	prev := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(core))
	defer logger.SetGlobalLogger(prev)

	captureErr := errors.New("Capture image not found.")
	failing := false
	for _, err := range []error{captureErr, captureErr, captureErr, nil, nil, captureErr} {
		failing = logCaptureResult(err, failing)
	}

	assert.True(t, failing)
	assert.Equal(t, 2, logs.FilterLevelExact(zapcore.WarnLevel).Len(), "one warning per failure streak")
	assert.Equal(t, 1, logs.FilterMessage("screenshot capture recovered").Len())
}
