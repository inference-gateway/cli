package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"slices"
	"sync"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
)

// ChildStderrJSONEnv is the environment variable the daemon sets on the
// processes it starts. A child process then logs JSON to stderr and keeps no
// log file of its own: the daemon collects its stderr into the daemon log.
const ChildStderrJSONEnv = "INFER_LOG_STDERR_JSON"

// stderrJSONMode reports whether this process was started by the daemon, so
// the logger writes JSON to stderr instead of a log file. The answer is read
// once, since reading it clears the flag.
var stderrJSONMode = sync.OnceValue(consumeStderrJSONFlag)

// consumeStderrJSONFlag reads the daemon's flag and clears it, so the
// processes this one starts keep their own log files: nobody collects their
// stderr.
func consumeStderrJSONFlag() bool {
	enabled := os.Getenv(ChildStderrJSONEnv) == "true"
	_ = os.Unsetenv(ChildStderrJSONEnv)
	return enabled
}

// stderrJSONLogger builds the production logger of a daemon-started child:
// JSON lines on stderr the daemon collects, no log file of its own.
func stderrJSONLogger(cfg Config) (*zap.Logger, error) {
	zapCfg := zap.NewProductionConfig()
	zapCfg.Sampling = nil
	zapCfg.OutputPaths = []string{"stderr"}
	zapCfg.ErrorOutputPaths = []string{"stderr"}
	if cfg.Verbose || cfg.Debug {
		zapCfg.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	}
	return zapCfg.Build(zap.AddCallerSkip(1))
}

// CollectChildStderr logs one started child process's stderr into this
// process's log, one entry per line, enriched with the collector's tags
// (typically project_dir, conversation_id and worker_pid). It returns once
// the child's stderr closes, with the last line that was not JSON, which is
// the child's own plain error when it failed.
func CollectChildStderr(stderr io.Reader, tags ...any) string {
	collector := GetGlobalLogger().WithOptions(zap.WithCaller(false), zap.AddStacktrace(zapcore.DPanicLevel)).Sugar()
	reader := bufio.NewReader(stderr)
	lastPlainLine := ""
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 && !logChildEntry(collector, line, tags) {
			lastPlainLine = string(line)
			collector.Warnw("child stderr line is not JSON", append(slices.Clone(tags), "line", lastPlainLine)...)
		}
		if err != nil {
			return lastPlainLine
		}
	}
}

// logChildEntry merges one JSON line the child logged into an entry of this
// process's log carrying the collector's tags: the child's message, fields
// and level, with the collector's own timestamp. It reports false for a line
// that is not JSON.
func logChildEntry(collector *zap.SugaredLogger, line []byte, tags []any) bool {
	var entry map[string]any
	if json.Unmarshal(line, &entry) != nil {
		return false
	}
	level, msg := childLevel(entry), childMsg(entry, line)
	keysAndValues := make([]any, 0, 2*len(entry)+len(tags))
	for key, value := range entry {
		if key != "level" && key != "msg" && key != "ts" {
			keysAndValues = append(keysAndValues, key, value)
		}
	}
	collector.Logw(level, msg, append(keysAndValues, tags...)...)
	return true
}

// childLevel maps the child entry's level, falling back to info. It stops at
// error, because logging a fatal or panic entry would end this process too.
func childLevel(entry map[string]any) zapcore.Level {
	level := zapcore.InfoLevel
	if text, ok := entry["level"].(string); ok {
		_ = level.UnmarshalText([]byte(text))
	}
	return min(level, zapcore.ErrorLevel)
}

// childMsg takes the child entry's message, falling back to the raw line.
func childMsg(entry map[string]any, line []byte) string {
	if msg, ok := entry["msg"].(string); ok {
		return msg
	}
	return string(line)
}
