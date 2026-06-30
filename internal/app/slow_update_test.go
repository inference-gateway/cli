package app

import (
	"testing"
	"time"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"

	constants "github.com/inference-gateway/cli/internal/constants"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// logSlowUpdate is the single-ingress instrumentation: a handler slower than
// SlowUpdateThreshold warns once, tagged with the event type; a fast handler is
// silent.
func TestLogSlowUpdate(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	prev := logger.GetGlobalLogger()
	logger.SetGlobalLogger(zap.New(core))
	defer logger.SetGlobalLogger(prev)

	logSlowUpdate(time.Now().Add(-2*constants.SlowUpdateThreshold), domain.DrainQueueEvent{})
	logSlowUpdate(time.Now(), domain.DrainQueueEvent{})

	warns := logs.FilterMessage("slow update").All()
	if len(warns) != 1 {
		t.Fatalf("expected exactly 1 'slow update' warning, got %d", len(warns))
	}
	if ev := warns[0].ContextMap()["event"]; ev != "domain.DrainQueueEvent" {
		t.Errorf("event field = %v, want domain.DrainQueueEvent", ev)
	}
}
