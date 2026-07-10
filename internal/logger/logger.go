package logger

import (
	"compress/gzip"
	"fmt"
	"io"
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
	Verbose          bool
	Debug            bool
	LogDir           string
	Stdout           bool
	ArchiveEnabled   bool
	ArchiveMaxSizeMB int
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

	if cfg.ArchiveEnabled && cfg.ArchiveMaxSizeMB > 0 {
		if err := archiveLogFile(logFile, cfg.ArchiveMaxSizeMB); err != nil {
			fmt.Fprintf(os.Stderr, "failed to archive log file %s: %v\n", logFile, err)
		}
	}

	zapCfg := zap.NewProductionConfig()
	zapCfg.OutputPaths = []string{logFile}
	zapCfg.ErrorOutputPaths = []string{logFile}

	if cfg.Stdout {
		zapCfg.OutputPaths = append(zapCfg.OutputPaths, "stdout")
		zapCfg.ErrorOutputPaths = append(zapCfg.ErrorOutputPaths, "stderr")
	}

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

// SetGlobalLogger swaps the global logger (and its sugared form). It is primarily
// a test seam: tests build a logger over an observed core to assert what was
// logged. Passing nil is a no-op.
func SetGlobalLogger(l *zap.Logger) {
	if l == nil {
		return
	}
	globalLogger = l
	sugar = l.Sugar()
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

// archiveLogFile checks if the given log file exceeds maxSizeMB. If it does,
// the file is gzip-compressed and renamed with a timestamp suffix, then
// truncated so the logger can continue writing to the original path.
func archiveLogFile(path string, maxSizeMB int) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to archive
		}
		return fmt.Errorf("stat: %w", err)
	}

	maxBytes := int64(maxSizeMB) * 1024 * 1024
	if info.Size() <= maxBytes {
		return nil // below threshold
	}

	// Open the file for reading
	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = src.Close() }()

	// Build the archive path: app-{date}-{timestamp}.log.gz
	ts := time.Now().Unix()
	archivePath := path + fmt.Sprintf(".%d.gz", ts)

	dst, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("create archive %s: %w", archivePath, err)
	}
	defer func() { _ = dst.Close() }()

	gw := gzip.NewWriter(dst)
	if _, err := io.Copy(gw, src); err != nil {
		_ = gw.Close()
		return fmt.Errorf("compress: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}

	// Truncate the original file so the logger starts fresh
	if err := os.Truncate(path, 0); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	return nil
}
