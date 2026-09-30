package logger

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
)

// ChildStderrJSONEnv is the environment variable the daemon sets on the
// processes it starts. A child process then logs JSON to stderr and keeps no
// log file of its own: the daemon collects its stderr into the daemon log.
const ChildStderrJSONEnv = "INFER_LOG_STDERR_JSON"

// StderrJSONMode reports whether this process was started by the daemon, so
// the logger writes JSON to stderr instead of a log file.
func StderrJSONMode() bool {
	return os.Getenv(ChildStderrJSONEnv) == "true"
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
// (typically project_dir, conversation_id and worker_pid). A line the child
// logged as JSON keeps its message, fields and level, anything else logs
// as-is. It returns once the child's stderr closes.
func CollectChildStderr(stderr io.Reader, tags ...any) {
	reader := bufio.NewReader(stderr)
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			logChildLine(line, tags)
		}
		if err != nil {
			return
		}
	}
}

// logChildLine logs one child stderr line: JSON the child logged merges into
// one entry of this process's log carrying the collector's tags, anything
// else logs the raw line.
func logChildLine(line []byte, tags []any) {
	var entry map[string]any
	if json.Unmarshal(line, &entry) != nil {
		Warn("child stderr line is not JSON", append(slicesClone(tags), "line", string(line))...)
		return
	}
	fields := append(childFields(entry), pairFields(tags)...)
	GetGlobalLogger().Log(childLevel(entry), childMsg(entry, line), fields...)
}

// pairFields turns the collector's key/value tag pairs into log fields.
func pairFields(pairs []any) []zap.Field {
	fields := make([]zap.Field, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		fields = append(fields, zap.Any(key, pairs[i+1]))
	}
	return fields
}

// childFields copies the child entry's own fields, dropping the ones the
// collector replaces with its own entry: the child's timestamp and the
// message and level that move into the entry proper.
func childFields(entry map[string]any) []zap.Field {
	fields := make([]zap.Field, 0, len(entry))
	for key, value := range entry {
		switch key {
		case "level", "msg", "ts":
			continue
		}
		fields = append(fields, zap.Any(key, value))
	}
	return fields
}

// childLevel maps the child entry's level, falling back to info.
func childLevel(entry map[string]any) zapcore.Level {
	level := zapcore.InfoLevel
	if text, ok := entry["level"].(string); ok {
		_ = level.UnmarshalText([]byte(text))
	}
	return level
}

// childMsg takes the child entry's message, falling back to the raw line.
func childMsg(entry map[string]any, line []byte) string {
	if msg, ok := entry["msg"].(string); ok {
		return msg
	}
	return string(line)
}

func slicesClone(values []any) []any {
	return append([]any{}, values...)
}
