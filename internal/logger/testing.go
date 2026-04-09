package logger

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestLogger creates a logger that captures logs for assertions
// The returned ObservedLogs can be used to verify log messages in tests
func TestLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zap.DebugLevel)
	logger := zap.New(core)
	return logger, logs
}

// TestContext creates a context with a test logger
// Returns both the context and the observed logs for assertions
func TestContext() (context.Context, *observer.ObservedLogs) {
	logger, logs := TestLogger()
	ctx := ContextWithLogger(context.Background(), logger)
	return ctx, logs
}

// NopContext creates a context with a no-op logger (for benchmarks or when logs are not needed)
func NopContext() context.Context {
	return ContextWithLogger(context.Background(), zap.NewNop())
}
