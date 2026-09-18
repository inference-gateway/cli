package insights

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// logLine renders one zap production record the way the logger writes it: ts is
// epoch seconds as a JSON number, level is lowercase.
func logLine(level string, when time.Time, msg, errMsg string) string {
	return fmt.Sprintf(`{"level":%q,"ts":%f,"caller":"container/container.go:534","msg":%q,"error":%q}`,
		level, float64(when.UnixNano())/float64(time.Second), msg, errMsg) + "\n"
}

func writeLog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeGzLog(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	fh, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fh.Close() }()
	gz := gzip.NewWriter(fh)
	if _, err := gz.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestCollectLogsFoldsAndCounts is the core of the feature: lines that differ
// only in a port, a path or a request id are one recurring failure, not four
// singletons, and the count is what tells a retry loop from a one-off.
func TestCollectLogsFoldsAndCounts(t *testing.T) {
	dir := t.TempDir()
	base := time.Now().Add(-time.Hour).Truncate(time.Second)

	var live strings.Builder
	live.WriteString(logLine("info", base, "initialized conversation storage", ""))
	live.WriteString(logLine("warn", base.Add(1*time.Second), "failed to start gateway container", "port 8080 already in use"))
	live.WriteString(logLine("warn", base.Add(2*time.Second), "failed to start gateway container", "port 8081 already in use"))
	live.WriteString(logLine("error", base.Add(3*time.Second), "open /Users/a/.infer/config.yaml", "no such file"))
	writeLog(t, dir, "app-2026-09-18.log", live.String())

	writeLog(t, dir, "daemon-2026-09-18.log",
		logLine("warn", base.Add(4*time.Second), "failed to start gateway container", "port 9000 already in use"))
	writeGzLog(t, dir, "app-2026-09-17.log.1758153600.gz",
		logLine("warn", base.Add(5*time.Second), "failed to start gateway container", "port 7000 already in use"))
	writeLog(t, dir, "gateway-2026-09-18.log", "panic: raw subprocess output, not zap json\n")

	digest := collectLogs(dir, time.Time{}, "warn")

	if digest.Scanned != 5 {
		t.Fatalf("expected 5 ingested records (info and the gateway log excluded), got %d", digest.Scanned)
	}
	if len(digest.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d: %+v", len(digest.Groups), digest.Groups)
	}

	top := digest.Groups[0]
	if top.Count != 4 {
		t.Errorf("the four port variants must fold into one group of 4, got %d: %+v", top.Count, digest.Groups)
	}
	if !strings.Contains(top.Sample, "failed to start gateway container") {
		t.Errorf("group must keep a verbatim sample, got %q", top.Sample)
	}
	if !top.First.Equal(base.Add(1 * time.Second)) {
		t.Errorf("first seen = %s, want %s", top.First, base.Add(1*time.Second))
	}
	if !top.Last.Equal(base.Add(5 * time.Second)) {
		t.Errorf("last seen = %s, want %s", top.Last, base.Add(5*time.Second))
	}
}

// TestCollectLogsHonoursLevelAndWindow pins the two filters that keep the digest
// about failures rather than routine lifecycle chatter.
func TestCollectLogsHonoursLevelAndWindow(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)

	writeLog(t, dir, "app-2026-09-18.log",
		logLine("info", now, "initialized conversation storage", "")+
			logLine("warn", now.Add(-48*time.Hour), "stale warning outside the window", "")+
			logLine("error", now, "gateway health check failed", "connection refused"))

	windowed := collectLogs(dir, now.Add(-24*time.Hour), "warn")
	if windowed.Scanned != 1 || len(windowed.Groups) != 1 {
		t.Fatalf("expected only the in-window error, got %d records / %d groups: %+v",
			windowed.Scanned, len(windowed.Groups), windowed.Groups)
	}
	if !strings.Contains(windowed.Groups[0].Sample, "gateway health check failed") {
		t.Errorf("wrong record survived: %q", windowed.Groups[0].Sample)
	}

	if all := collectLogs(dir, time.Time{}, "info"); all.Scanned != 3 {
		t.Errorf("a lower floor over an open window must ingest all 3, got %d", all.Scanned)
	}
	if bad := collectLogs(dir, time.Time{}, "nonsense"); bad.Scanned != 2 {
		t.Errorf("an unrecognized level must fall back to warn (2 records), got %d", bad.Scanned)
	}
}

// TestCollectLogsBounded is the token-cost pin: a large log with far more
// distinct templates than the caps must still produce a small digest.
func TestCollectLogsBounded(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)

	var body strings.Builder
	for i := range 50000 {
		body.WriteString(logLine("error", now,
			fmt.Sprintf("failure kind %s of %d", strings.Repeat("z", 400), i%6000), strings.Repeat("q", 400)))
	}
	writeLog(t, dir, "app-2026-09-18.log", body.String())

	digest := collectLogs(dir, time.Time{}, "warn")

	if len(digest.Groups) > maxLogGroups {
		t.Errorf("digest not bounded: %d groups", len(digest.Groups))
	}
	for _, g := range digest.Groups {
		if len(g.Sample) > maxLogSampleChars+3 {
			t.Errorf("sample not bounded: %d chars", len(g.Sample))
		}
	}
	if got := len(buildDigest(nil, nil, nil, "", digest)); got > maxDigestChars+3 {
		t.Errorf("prompt payload not bounded: %d chars", got)
	}
}

// TestLogFailureWithoutConversationReachesDigest is the load-bearing one: a
// startup crash never produces a conversation entry, so before this the report
// could not see it at all.
func TestLogFailureWithoutConversationReachesDigest(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)

	var body strings.Builder
	for range 400 {
		body.WriteString(logLine("error", now, "failed to start gateway container", "port 8080 already in use"))
	}
	writeLog(t, dir, "app-2026-09-18.log", body.String())

	digest := collectLogs(dir, time.Time{}, "warn")
	if digest.Scanned != 400 || len(digest.Groups) != 1 || digest.Groups[0].Count != 400 {
		t.Fatalf("expected 400 records folded into one group of 400, got %d / %+v", digest.Scanned, digest.Groups)
	}

	prompt := buildDigest(nil, nil, nil, "", digest)
	for _, want := range []string{"LOG FAILURES", "x400", "failed to start gateway container", "port 8080 already in use"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("digest missing %q:\n%s", want, prompt)
		}
	}

	report := renderReport(reportMeta{Generated: now, LogRecords: digest.Scanned, LogGroups: len(digest.Groups)}, nil, nil, digest, "x")
	for _, want := range []string{"log_records: 400", "log_groups: 1", "## Log failures", "| 400 |"} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q:\n%s", want, report)
		}
	}
}

// TestCollectLogsDegrades covers every way the log dir can disappoint: absent,
// empty, holding garbage, or holding a line torn mid-write because the live file
// is still being appended to by this very process.
func TestCollectLogsDegrades(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	if got := collectLogs(filepath.Join(t.TempDir(), "nope"), time.Time{}, "warn"); got.Scanned != 0 || len(got.Groups) != 0 {
		t.Errorf("a missing dir must yield an empty digest, got %+v", got)
	}
	if got := collectLogs(t.TempDir(), time.Time{}, "warn"); got.Scanned != 0 || len(got.Groups) != 0 {
		t.Errorf("an empty dir must yield an empty digest, got %+v", got)
	}

	dir := t.TempDir()
	writeLog(t, dir, "app-2026-09-18.log",
		"not json at all\n"+
			`{"level":"error"`+"\n"+
			`{"level":"error","ts":"not-a-number","msg":"bad ts"}`+"\n"+
			`{"hello":"world"}`+"\n"+
			logLine("error", now, "the one good record", "boom"))
	writeGzLog(t, dir, "app-2026-09-17.log.1.gz", "")
	writeLog(t, dir, "app-2026-09-16.log.2.gz", "this is not gzip")

	got := collectLogs(dir, time.Time{}, "warn")
	if got.Scanned != 1 || len(got.Groups) != 1 {
		t.Fatalf("malformed lines must be skipped, leaving 1 record, got %d / %+v", got.Scanned, got.Groups)
	}
	if !strings.Contains(got.Groups[0].Sample, "the one good record") {
		t.Errorf("wrong record survived: %q", got.Groups[0].Sample)
	}
}

// TestNormalizeLogMasksNoise pins what folds and what must not: two failures
// differing only in a path or an id are the same failure, two different messages
// are not.
func TestNormalizeLogMasksNoise(t *testing.T) {
	tests := []struct {
		name  string
		a, b  string
		equal bool
	}{
		{"paths", "open /Users/a/x.go: no such file", "open /home/b/y.go: no such file", true},
		{"numbers", "dial tcp 127.0.0.1:8080: refused", "dial tcp 10.0.0.2:9090: refused", true},
		{"hex ids", "session 3f9a2b7c1d4e5f60 aborted", "session 0011223344556677 aborted", true},
		{"distinct messages", "failed to start gateway", "failed to stop gateway", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLog(tt.a) == normalizeLog(tt.b); got != tt.equal {
				t.Errorf("normalizeLog(%q)=%q vs normalizeLog(%q)=%q: equal=%v, want %v",
					tt.a, normalizeLog(tt.a), tt.b, normalizeLog(tt.b), got, tt.equal)
			}
		})
	}
}
