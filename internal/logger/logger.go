package logger

import (
	"fmt"
	"os"
	"time"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"

	config "github.com/inference-gateway/cli/config"
)

var (
	globalLogger *zap.Logger
	sugar        *zap.SugaredLogger
)

// Config for logger initialization
type Config struct {
	Verbose bool
	Debug   bool
	LogDir  string
}

// Init initializes the global logger (for migration period)
func Init(cfg Config) {
	var err error
	globalLogger, err = NewLogger(cfg)
	if err != nil {
		globalLogger = zap.NewNop()
	}
	sugar = globalLogger.Sugar()
	zap.ReplaceGlobals(globalLogger)
	zap.RedirectStdLog(globalLogger)
}

// NewLogger creates a new configured logger instance
func NewLogger(cfg Config) (*zap.Logger, error) {
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = config.DefaultLogsPath
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return zap.NewNop(), err
	}

	logFile := fmt.Sprintf("%s/app-%s.log", logDir, time.Now().Format("2006-01-02"))

	zapCfg := zap.NewProductionConfig()
	zapCfg.OutputPaths = []string{logFile}
	zapCfg.ErrorOutputPaths = []string{logFile}

	if cfg.Verbose || cfg.Debug {
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	}

	return zapCfg.Build(zap.AddCallerSkip(1))
}

// GetGlobalLogger returns the global logger instance
// Useful for services that need to store a logger reference
func GetGlobalLogger() *zap.Logger {
	if globalLogger == nil {
		return zap.L()
	}
	return globalLogger
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	if sugar != nil {
		if len(args) > 0 {
			sugar.Debugw(msg, args...)
		} else {
			sugar.Debug(msg)
		}
	}
}

// Info logs an info message
func Info(msg string, args ...any) {
	if sugar != nil {
		if len(args) > 0 {
			sugar.Infow(msg, args...)
		} else {
			sugar.Info(msg)
		}
	}
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	if sugar != nil {
		if len(args) > 0 {
			sugar.Warnw(msg, args...)
		} else {
			sugar.Warn(msg)
		}
	}
}

// Error logs an error message
func Error(msg string, args ...any) {
	if sugar != nil {
		if len(args) > 0 {
			sugar.Errorw(msg, args...)
		} else {
			sugar.Error(msg)
		}
	}
}

// Fatal logs a fatal message and exits
func Fatal(msg string, args ...any) {
	if sugar != nil {
		if len(args) > 0 {
			sugar.Fatalw(msg, args...)
		} else {
			sugar.Fatal(msg)
		}
	}
	os.Exit(1)
}

// Sync flushes any buffered log entries
func Sync() error {
	if globalLogger != nil {
		return globalLogger.Sync()
	}
	return nil
}

// Close closes the logger and flushes any buffered entries
func Close() {
	if globalLogger != nil {
		_ = globalLogger.Sync()
	}
}
