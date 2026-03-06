package logger

import (
	"context"

	"go.uber.org/zap"
)

type ctxLoggerKey struct{}

// ContextWithLogger attaches a logger to the context
func ContextWithLogger(ctx context.Context, l *zap.Logger) context.Context {
	return context.WithValue(ctx, ctxLoggerKey{}, l)
}

// FromContext retrieves the logger from context
// Falls back to global logger during migration period
func FromContext(ctx context.Context) *zap.Logger {
	if l, ok := ctx.Value(ctxLoggerKey{}).(*zap.Logger); ok {
		return l
	}
	return zap.L()
}

// L is a shorthand for FromContext (matches zap.L() pattern)
func L(ctx context.Context) *zap.Logger {
	return FromContext(ctx)
}

// With creates a child context with additional logger fields
func With(ctx context.Context, fields ...zap.Field) context.Context {
	return ContextWithLogger(ctx, FromContext(ctx).With(fields...))
}

// Sugar returns a sugared logger from context
func Sugar(ctx context.Context) *zap.SugaredLogger {
	return FromContext(ctx).Sugar()
}
