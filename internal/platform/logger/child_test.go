package logger

import (
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	zap "go.uber.org/zap"
	zapcore "go.uber.org/zap/zapcore"
	observer "go.uber.org/zap/zaptest/observer"
)

func TestCollectChildStderr(t *testing.T) {
	core, entries := observer.New(zapcore.DebugLevel)
	original := GetGlobalLogger()
	defer SetGlobalLogger(original)
	SetGlobalLogger(zap.New(core))

	stderr := strings.Join([]string{
		`{"level":"warn","msg":"worker degraded","mode":"auto"}`,
		"panic: not json at all",
		"",
		`{"level":"info","msg":"worker ready"}`,
		`{"level":"fatal","msg":"worker gave up"}`,
		`{"level":"panic","msg":"worker panicked"}`,
	}, "\n") + "\n"
	tags := []any{"project_dir", "/proj", "conversation_id", "conv-1", "worker_pid", 4242}

	failure := CollectChildStderr(strings.NewReader(stderr), tags...)

	if failure != "panic: not json at all" {
		t.Fatalf("failure = %q, want the plain output", failure)
	}
	if got := len(entries.All()); got != 5 {
		t.Fatalf("got %d entries, want 5: %v", got, entries.All())
	}
	for _, terminal := range entries.All()[2:4] {
		if terminal.Level != zapcore.ErrorLevel {
			t.Fatalf("%q kept the terminal level %s", terminal.Message, terminal.Level)
		}
	}
	merged := entries.All()[0]
	if merged.Message != "worker degraded" || merged.Level != zapcore.WarnLevel {
		t.Fatalf("json line not merged: %q at %s", merged.Message, merged.Level)
	}
	fields := merged.ContextMap()
	if fields["mode"] != "auto" || fields["project_dir"] != "/proj" || fields["conversation_id"] != "conv-1" {
		t.Fatalf("merged entry lost fields: %v", fields)
	}
	if pid, ok := fields["worker_pid"].(int64); !ok || pid != 4242 {
		t.Fatalf("worker_pid not tagged: %v", fields["worker_pid"])
	}
	if _, ok := fields["ts"]; ok {
		t.Fatalf("merged entry kept the child timestamp: %v", fields)
	}
	raw := entries.All()[4]
	if raw.Message != "child wrote plain lines to stderr" || raw.Level != zapcore.WarnLevel {
		t.Fatalf("plain output not reported once at the end: %q", raw.Message)
	}
	if raw.ContextMap()["output"] != "panic: not json at all" || raw.ContextMap()["lines"] != int64(1) {
		t.Fatalf("plain output missing: %v", raw.ContextMap())
	}
	if ready := entries.All()[1]; ready.Message != "worker ready" || ready.Level != zapcore.InfoLevel {
		t.Fatalf("second entry wrong: %q at %s", ready.Message, ready.Level)
	}
}

func TestCollectChildStderrReturnsTheReportedExitError(t *testing.T) {
	core, entries := observer.New(zapcore.DebugLevel)
	original := GetGlobalLogger()
	defer SetGlobalLogger(original)
	SetGlobalLogger(zap.New(core))
	t.Setenv(ChildStderrJSONEnv, "true")
	stderrJSONMode = sync.OnceValue(consumeStderrJSONFlag)

	var stderr strings.Builder
	stderr.WriteString("stray line one\nstray line two\n")
	if !ReportExitError(&stderr, errors.New(`Model "sonnet" not available. Available: [a b c]`)) {
		t.Fatal("the exit error was not reported as JSON")
	}

	failure := CollectChildStderr(strings.NewReader(stderr.String()), "conversation_id", "conv-1")

	if failure != `Model "sonnet" not available. Available: [a b c]` {
		t.Fatalf("failure = %q, want the whole reported error", failure)
	}
	if got := len(entries.All()); got != 2 {
		t.Fatalf("got %d entries, want the exit error and one warning: %v", got, entries.All())
	}
	if exit := entries.All()[0]; exit.Level != zapcore.ErrorLevel || exit.ContextMap()["conversation_id"] != "conv-1" {
		t.Fatalf("exit error not merged with tags: %v", exit)
	}
	if warning := entries.All()[1]; warning.ContextMap()["lines"] != int64(2) {
		t.Fatalf("stray lines not reported once: %v", warning.ContextMap())
	}
}

func TestReportExitErrorLeavesPeopleTheStyledError(t *testing.T) {
	stderrJSONMode = sync.OnceValue(consumeStderrJSONFlag)
	var stderr strings.Builder
	if ReportExitError(&stderr, errors.New("boom")) || stderr.Len() != 0 {
		t.Fatalf("reported JSON outside a daemon-started child: %q", stderr.String())
	}
}

func TestConsumeStderrJSONFlag(t *testing.T) {
	t.Setenv(ChildStderrJSONEnv, "true")
	if !consumeStderrJSONFlag() {
		t.Fatal("the flag is set but was not read")
	}
	if _, inherited := os.LookupEnv(ChildStderrJSONEnv); inherited {
		t.Fatal("the flag is still set for the processes this one starts")
	}
	if consumeStderrJSONFlag() {
		t.Fatal("the flag was read while unset")
	}
}
