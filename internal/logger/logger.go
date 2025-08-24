package logger

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger *zap.Logger
	sugar  *zap.SugaredLogger
	logDir string
)

// ConfigProvider interface for getting configuration values
type ConfigProvider interface {
	GetLogDir() string
	IsDebugMode() bool
}

// Init initializes the logger with the specified verbose level and config
func Init(verbose bool, cfg ConfigProvider) {
	if cfg != nil {
		logDir = cfg.GetLogDir()
		verbose = verbose || cfg.IsDebugMode()
	} else {
		logDir = ".infer/logs"
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		logger = zap.NewNop()
		sugar = logger.Sugar()
		return
	}

	timestamp := time.Now().Format("2006-01-02")
	errorLogPath := filepath.Join(logDir, fmt.Sprintf("error-%s.log", timestamp))
	infoLogPath := filepath.Join(logDir, fmt.Sprintf("info-%s.log", timestamp))
	debugLogPath := filepath.Join(logDir, fmt.Sprintf("debug-%s.log", timestamp))

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	encoder := zapcore.NewJSONEncoder(encoderConfig)

	errorFile, err := os.OpenFile(errorLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logger = zap.NewNop()
		sugar = logger.Sugar()
		return
	}

	infoFile, err := os.OpenFile(infoLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		_ = errorFile.Close()
		logger = zap.NewNop()
		sugar = logger.Sugar()
		return
	}

	var cores []zapcore.Core

	errorCore := zapcore.NewCore(
		encoder,
		zapcore.AddSync(errorFile),
		zapcore.ErrorLevel,
	)
	cores = append(cores, errorCore)

	infoCore := zapcore.NewCore(
		encoder,
		zapcore.AddSync(infoFile),
		zapcore.InfoLevel,
	)
	cores = append(cores, infoCore)

	if verbose {
		debugFile, err := os.OpenFile(debugLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			debugCore := zapcore.NewCore(
				encoder,
				zapcore.AddSync(debugFile),
				zapcore.DebugLevel,
			)
			cores = append(cores, debugCore)
		}
	}

	core := zapcore.NewTee(cores...)

	logger = zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
	sugar = logger.Sugar()

	zap.ReplaceGlobals(logger)
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

// Error logs an error message to the error log file
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
