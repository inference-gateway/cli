package shortcuts

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	maxLogGroups      = 25
	maxLogTemplates   = 5000
	maxLogSampleChars = 200
	logScanBuffer     = 10 * 1024 * 1024
)

// logLevels is zap's lowercase severity order. A record is ingested when its
// index is at or above the configured minimum - cheaper than importing zapcore
// to parse a string the JSON already hands us as a string.
var logLevels = []string{"debug", "info", "warn", "error", "dpanic", "panic", "fatal"}

// logNoise masks what varies per occurrence - absolute paths and hex ids - so
// "open /a/b.go: no such file" and "open /c/d.go: no such file" fold into one
// group. digitRun then takes the numbers (ports, durations, exit codes).
var logNoise = regexp.MustCompile(`(/[\w.\-/]+)|(\b[0-9a-f]{8,}\b)`)

// logGroup is one normalized log template and how often it recurred. Count is
// the load-bearing field: a high count over a short span is a retry loop, the
// same count spread over days is a chronic fault.
type logGroup struct {
	Template string
	Count    int
	First    time.Time
	Last     time.Time
	Sample   string
}

// logDigest is the bounded summary of a log directory. Scanned counts the
// records that passed the level and window filters, so Scanned vs len(Groups)
// is the compaction ratio the report states.
type logDigest struct {
	Scanned int
	Groups  []logGroup
}

// logRecord is a minimal decode of one zap production line. ts is epoch seconds
// as a JSON number, not RFC3339. The error field is what the sugared `...w`
// helpers attach, and folding on msg+error is what makes a group actionable -
// "tool execution failed" alone says nothing.
type logRecord struct {
	Level string  `json:"level"`
	TS    float64 `json:"ts"`
	Msg   string  `json:"msg"`
	Error string  `json:"error"`
}

// collectLogs folds the structured logs under dir into a bounded digest of
// recurring failures. It answers the question the conversation store cannot: what
// went wrong without ever reaching a saved session - a startup crash, a gateway
// that never came up, a background job that died. An unreadable or absent dir
// costs this section, never the report, so it returns no error.
//
// Only app-*.log and daemon-*.log (and their gzip archives) are read; the
// sibling gateway-*.log is raw subprocess stdout with no level or timestamp to
// filter on. The gateway's own failures still land here, because the gateway
// manager logs them through zap.
//
// ponytail: one sequential pass. A log dir is a handful of daily files, so
// coordinating goroutines would cost more than the read; fan out over files with
// an errgroup if a dir ever outgrows it.
func collectLogs(dir string, since time.Time, minLevel string) logDigest {
	minRank := slices.Index(logLevels, strings.ToLower(strings.TrimSpace(minLevel)))
	if minRank < 0 {
		minRank = slices.Index(logLevels, "warn")
	}

	digest := logDigest{}
	groups := map[string]*logGroup{}
	for _, path := range structuredLogFiles(dir) {
		if info, err := os.Stat(path); err != nil || (!since.IsZero() && info.ModTime().Before(since)) {
			continue
		}
		foldLogFile(path, since, minRank, groups, &digest.Scanned)
	}

	digest.Groups = topLogGroups(groups)
	return digest
}

// structuredLogFiles lists the zap-formatted logs in dir, live files and gzip
// archives alike. A bad glob pattern or a missing dir yields nothing.
func structuredLogFiles(dir string) []string {
	var files []string
	for _, pattern := range []string{"app-*.log", "app-*.log.*.gz", "daemon-*.log", "daemon-*.log.*.gz"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			continue
		}
		files = append(files, matches...)
	}
	slices.Sort(files)
	return files
}

// foldLogFile folds one file's records into groups. Every failure is skipped
// rather than reported: the live log is being appended to by this very process,
// so its last line can be torn mid-write, and one bad file must not sink the
// digest.
func foldLogFile(path string, since time.Time, minRank int, groups map[string]*logGroup, scanned *int) {
	fh, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = fh.Close() }()

	var reader io.Reader = fh
	if strings.HasSuffix(path, ".gz") {
		gz, gzErr := gzip.NewReader(fh)
		if gzErr != nil {
			return
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	}

	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), logScanBuffer)
	for scanner.Scan() {
		var rec logRecord
		if json.Unmarshal(scanner.Bytes(), &rec) != nil {
			continue
		}
		if rec.Msg == "" || slices.Index(logLevels, rec.Level) < minRank {
			continue
		}
		when := time.Unix(0, int64(rec.TS*float64(time.Second)))
		if !since.IsZero() && when.Before(since) {
			continue
		}
		*scanned++
		addLogRecord(groups, rec, when)
	}

	_ = scanner.Err()
}

// addLogRecord counts one record against its template.
//
// ponytail: past maxLogTemplates distinct templates only existing keys
// increment, so a pathological log cannot grow the map without bound. Swap in an
// LRU if the dropped tail ever turns out to matter.
func addLogRecord(groups map[string]*logGroup, rec logRecord, when time.Time) {
	raw := strings.TrimSpace(rec.Msg + " " + rec.Error)
	key := normalizeLog(raw)

	g, ok := groups[key]
	if !ok {
		if len(groups) >= maxLogTemplates {
			return
		}
		g = &logGroup{Template: key, First: when, Last: when, Sample: oneLine(raw, maxLogSampleChars)}
		groups[key] = g
	}
	g.Count++
	if when.Before(g.First) {
		g.First = when
	}
	if when.After(g.Last) {
		g.Last = when
	}
}

// normalizeLog reduces a line to the template it is an instance of, so repeats
// that differ only in a path, an id or a number fold into one group.
func normalizeLog(s string) string {
	masked := logNoise.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "X")
	return digitRun.ReplaceAllString(masked, "N")
}

// topLogGroups returns the most frequent groups, capped, ordered so the digest
// is deterministic.
func topLogGroups(groups map[string]*logGroup) []logGroup {
	out := make([]logGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b logGroup) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Template, b.Template)
	})
	return out[:min(len(out), maxLogGroups)]
}
