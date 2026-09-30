package logger

import (
	"strings"
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
	}, "\n") + "\n"
	tags := []any{"project_dir", "/proj", "conversation_id", "conv-1", "worker_pid", 4242}

	CollectChildStderr(strings.NewReader(stderr), tags...)

	if got := len(entries.All()); got != 3 {
		t.Fatalf("got %d entries, want 3: %v", got, entries.All())
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
	raw := entries.All()[1]
	if raw.Message != "child stderr line is not JSON" {
		t.Fatalf("raw line not reported: %q", raw.Message)
	}
	if raw.ContextMap()["line"] != "panic: not json at all" {
		t.Fatalf("raw line text missing: %v", raw.ContextMap())
	}
	if ready := entries.All()[2]; ready.Message != "worker ready" || ready.Level != zapcore.InfoLevel {
		t.Fatalf("third entry wrong: %q at %s", ready.Message, ready.Level)
	}
}

func TestStderrJSONMode(t *testing.T) {
	t.Setenv(ChildStderrJSONEnv, "true")
	if !StderrJSONMode() {
		t.Fatal("StderrJSONMode is false while the env flag is set")
	}
	t.Setenv(ChildStderrJSONEnv, "false")
	if StderrJSONMode() {
		t.Fatal("StderrJSONMode is true while the env flag is unset")
	}
}
