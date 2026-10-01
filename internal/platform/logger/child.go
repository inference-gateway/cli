package logger

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
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

// exitErrorMsg marks the JSON line a daemon-started child writes last, when
// its command fails: the collecting parent returns its error field as the
// child's failure.
const exitErrorMsg = "command failed"

// ReportExitError writes a failed command's error as one JSON log line on
// stderr when this process was started by the daemon, so the parent collects
// the whole message. It reports false when the error should be printed for a
// person instead.
func ReportExitError(w io.Writer, err error) bool {
	if !stderrJSONMode() {
		return false
	}
	line, _ := json.Marshal(map[string]string{"level": "error", "msg": exitErrorMsg, "error": err.Error()})
	_, _ = fmt.Fprintln(w, string(line))
	return true
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
// the child's stderr closes, with the child's failure: the error it reported
// through ReportExitError, or else whatever plain lines it wrote. Plain lines
// are logged as one warning per child.
func CollectChildStderr(stderr io.Reader, tags ...any) string {
	collector := GetGlobalLogger().WithOptions(zap.WithCaller(false), zap.AddStacktrace(zapcore.DPanicLevel)).Sugar()
	reader := bufio.NewReader(stderr)
	exitError := ""
	var plainLines []string
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			reported, ok := logChildEntry(collector, line, tags)
			if !ok {
				plainLines = append(plainLines, string(line))
			}
			if reported != "" {
				exitError = reported
			}
		}
		if err != nil {
			break
		}
	}
	plainOutput := strings.Join(plainLines, "\n")
	if plainOutput != "" {
		collector.Warnw("child wrote plain lines to stderr", append(slices.Clone(tags), "lines", len(plainLines), "output", plainOutput)...)
	}
	return cmp.Or(exitError, plainOutput)
}

// logChildEntry merges one JSON line the child logged into an entry of this
// process's log carrying the collector's tags: the child's message, fields
// and level, with the collector's own timestamp. It reports false for a line
// that is not JSON, and the error of the child's exit error line.
func logChildEntry(collector *zap.SugaredLogger, line []byte, tags []any) (string, bool) {
	var entry map[string]any
	if json.Unmarshal(line, &entry) != nil {
		return "", false
	}
	level, msg := childLevel(entry), childMsg(entry, line)
	keysAndValues := make([]any, 0, 2*len(entry)+len(tags))
	for key, value := range entry {
		if key != "level" && key != "msg" && key != "ts" {
			keysAndValues = append(keysAndValues, key, value)
		}
	}
	collector.Logw(level, msg, append(keysAndValues, tags...)...)
	if msg != exitErrorMsg {
		return "", true
	}
	exitError, _ := entry["error"].(string)
	return exitError, true
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
