package logger

import (
	"log/slog"
	"os"
)

var logger *slog.Logger

// Init initializes the logger with the specified verbose level
func Init(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: level,
	}

	handler := slog.NewJSONHandler(os.Stderr, opts)
	logger = slog.New(handler)
	slog.SetDefault(logger)
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	if logger != nil {
		logger.Debug(msg, args...)
	}
}

// Info logs an info message
func Info(msg string, args ...any) {
	if logger != nil {
		logger.Info(msg, args...)
	}
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	if logger != nil {
		logger.Warn(msg, args...)
	}
}

// Error logs an error message
func Error(msg string, args ...any) {
	if logger != nil {
		logger.Error(msg, args...)
	}
}
