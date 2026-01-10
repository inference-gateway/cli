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
	logger *zap.Logger
	sugar  *zap.SugaredLogger
)

// Init initializes the logger with the specified verbose level, config, and optional console output
func Init(verbose, debug bool, logDir string, consoleOutput string) {
	verbose = verbose || debug

	var cfg zap.Config

	if consoleOutput == "stderr" {
		// Console mode: JSON output to stderr
		cfg = zap.Config{
			Level:            zap.NewAtomicLevelAt(getLogLevel(verbose)),
			Encoding:         "json",
			OutputPaths:      []string{"stderr"},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:        "timestamp",
				LevelKey:       "level",
				NameKey:        "logger",
				CallerKey:      "caller",
				MessageKey:     "msg",
				StacktraceKey:  "stacktrace",
				LineEnding:     zapcore.DefaultLineEnding,
				EncodeLevel:    zapcore.LowercaseLevelEncoder,
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeDuration: zapcore.SecondsDurationEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
			},
		}
	} else {
		// File mode: JSON output to log directory
		if logDir == "" {
			logDir = config.DefaultLogsPath
		}

		if err := os.MkdirAll(logDir, 0755); err != nil {
			logger = zap.NewNop()
			sugar = logger.Sugar()
			return
		}

		logFileName := fmt.Sprintf("app-%s.log", time.Now().Format("2006-01-02"))
		cfg = zap.Config{
			Level:            zap.NewAtomicLevelAt(getLogLevel(verbose)),
			Encoding:         "json",
			OutputPaths:      []string{logDir + "/" + logFileName},
			ErrorOutputPaths: []string{"stderr"},
			EncoderConfig: zapcore.EncoderConfig{
				TimeKey:        "timestamp",
				LevelKey:       "level",
				NameKey:        "logger",
				CallerKey:      "caller",
				MessageKey:     "msg",
				StacktraceKey:  "stacktrace",
				LineEnding:     zapcore.DefaultLineEnding,
				EncodeLevel:    zapcore.LowercaseLevelEncoder,
				EncodeTime:     zapcore.ISO8601TimeEncoder,
				EncodeDuration: zapcore.SecondsDurationEncoder,
				EncodeCaller:   zapcore.ShortCallerEncoder,
			},
		}
	}

	var err error
	logger, err = cfg.Build(zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
	if err != nil {
		logger = zap.NewNop()
		sugar = logger.Sugar()
		return
	}

	sugar = logger.Sugar()
	zap.ReplaceGlobals(logger)
}

func getLogLevel(verbose bool) zapcore.Level {
	if verbose {
		return zapcore.DebugLevel
	}
	return zapcore.InfoLevel
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
	if logger != nil {
		return logger.Sync()
	}
	return nil
}

// Close closes the logger and flushes any buffered entries
func Close() {
	if logger != nil {
		_ = logger.Sync()
	}
}
