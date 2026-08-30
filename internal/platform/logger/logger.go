package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
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
	FilePrefix       string
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
		logDir = config.DefaultLogsDir()
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return zap.NewNop(), err
	}

	prefix := cfg.FilePrefix
	if prefix == "" {
		prefix = "app"
	}
	logFile := fmt.Sprintf("%s/%s-%s.log", logDir, prefix, time.Now().Format("2006-01-02"))

	if cfg.ArchiveEnabled {
		if err := archiveLogFile(logFile, cfg.ArchiveMaxSizeMB); err != nil {
			fmt.Fprintf(os.Stderr, "failed to archive log file %s: %v\n", logFile, err)
		}
	}

	absLogFile, err := filepath.Abs(logFile)
	if err != nil {
		absLogFile = logFile
	}
	registerReopenSinkOnce.Do(func() {
		_ = zap.RegisterSink("reopen", func(u *url.URL) (zap.Sink, error) {
			return &reopenFileSink{path: u.Path}, nil
		})
	})

	zapCfg := zap.NewProductionConfig()
	zapCfg.OutputPaths = []string{"reopen://" + absLogFile}
	zapCfg.ErrorOutputPaths = []string{"reopen://" + absLogFile}

	if cfg.Stdout {
		zapCfg.OutputPaths = append(zapCfg.OutputPaths, "stdout")
		zapCfg.ErrorOutputPaths = append(zapCfg.ErrorOutputPaths, "stderr")
	}

	if cfg.Verbose || cfg.Debug {
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	}

	return zapCfg.Build(zap.AddCallerSkip(1))
}

// registerReopenSinkOnce guards zap's global sink registry: RegisterSink errors
// on a duplicate scheme and Init runs more than once (e.g. disableStdoutLogging).
var registerReopenSinkOnce sync.Once

// reopenFileSink is a zap sink that reopens its file (recreating the parent
// directory) whenever the path no longer exists, so deleting the logs directory
// mid-session resumes logging instead of writing to a dead fd forever.
// ponytail: one os.Stat per log write; buffer behind zapcore.BufferedWriteSyncer if it ever shows up in a profile.
type reopenFileSink struct {
	mu   sync.Mutex
	path string
	file *os.File
}

func (s *reopenFileSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureOpen(); err != nil {
		return 0, err
	}
	return s.file.Write(p)
}

func (s *reopenFileSink) ensureOpen() error {
	if s.file != nil {
		if _, err := os.Stat(s.path); err == nil {
			return nil
		}
		_ = s.file.Close()
		s.file = nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	s.file = f
	return nil
}

func (s *reopenFileSink) Sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	return s.file.Sync()
}

func (s *reopenFileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
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
	if maxSizeMB <= 0 {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat: %w", err)
	}

	maxBytes := int64(maxSizeMB) * 1024 * 1024
	if info.Size() <= maxBytes {
		return nil
	}

	src, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer func() { _ = src.Close() }()

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

	if err := os.Truncate(path, 0); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}

	return nil
}
