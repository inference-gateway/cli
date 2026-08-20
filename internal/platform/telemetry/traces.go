package telemetry

import (
	"bufio"
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TraceSession identifies one session with a non-empty local trace file
// (<dir>/<id>-traces.jsonl).
type TraceSession struct {
	ID       string    `json:"id"`
	Modified time.Time `json:"modified"`
}

// TraceSessions lists sessions with non-empty trace files under dir, newest
// first. Empty files (sessions that recorded no spans) are skipped.
func TraceSessions(dir string) ([]TraceSession, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*-traces.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []TraceSession
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || info.Size() == 0 {
			continue
		}
		out = append(out, TraceSession{
			ID:       strings.TrimSuffix(filepath.Base(f), "-traces.jsonl"),
			Modified: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

// TraceSpan is one node of a session's span tree.
type TraceSpan struct {
	Name       string            `json:"name"`
	Start      time.Time         `json:"start"`
	DurationMs float64           `json:"duration_ms"`
	Error      string            `json:"error,omitempty"`
	Attributes map[string]string `json:"attributes,omitempty"`
	Children   []*TraceSpan      `json:"children,omitempty"`
}

// traceLine is a minimal decode of one stdouttrace span stub line (the format
// written by the per-session trace file processor).
type traceLine struct {
	Name        string
	StartTime   time.Time
	EndTime     time.Time
	SpanContext struct{ SpanID string }
	Parent      struct{ SpanID string }
	Attributes  []otlpAttr
	Status      struct {
		Code        string
		Description string
	}
}

// LoadTraceTree reads a session's trace file and assembles the span tree.
// Spans whose parent is not in the file (the root, or orphans from a partial
// file) become roots; siblings are ordered by start time. Malformed lines are
// skipped best-effort.
func LoadTraceTree(dir, session string) ([]*TraceSpan, error) {
	fh, err := os.Open(filepath.Join(dir, session+"-traces.jsonl"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = fh.Close() }()

	type parented struct {
		span   *TraceSpan
		parent string
	}
	var nodes []parented
	byID := map[string]*TraceSpan{}

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var tl traceLine
		if json.Unmarshal(scanner.Bytes(), &tl) != nil || tl.Name == "" {
			continue
		}
		if id := tl.SpanContext.SpanID; id != "" && byID[id] != nil {
			continue
		}
		span := &TraceSpan{
			Name:       tl.Name,
			Start:      tl.StartTime,
			DurationMs: float64(tl.EndTime.Sub(tl.StartTime).Microseconds()) / 1000,
			Attributes: attrMap(tl.Attributes),
		}
		errType := span.Attributes["error.type"]
		if tl.Status.Code == "Error" {
			span.Error = cmp.Or(errType, tl.Status.Description, "error")
		} else {
			span.Error = errType
		}
		byID[tl.SpanContext.SpanID] = span
		nodes = append(nodes, parented{span, tl.Parent.SpanID})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	var roots []*TraceSpan
	orphans := map[string][]*TraceSpan{}
	for _, n := range nodes {
		switch p := byID[n.parent]; {
		case p != nil && p != n.span:
			p.Children = append(p.Children, n.span)
		case strings.Trim(n.parent, "0") == "":
			roots = append(roots, n.span)
		case n.span.Name == "session":
			roots = append(roots, n.span)
		default:
			orphans[n.parent] = append(orphans[n.parent], n.span)
		}
	}
	for _, kids := range orphans {
		root := &TraceSpan{Name: "session (in progress)", Children: kids}
		end := time.Time{}
		for i, k := range kids {
			if i == 0 || k.Start.Before(root.Start) {
				root.Start = k.Start
			}
			if kEnd := k.Start.Add(time.Duration(k.DurationMs * float64(time.Millisecond))); kEnd.After(end) {
				end = kEnd
			}
		}
		root.DurationMs = float64(end.Sub(root.Start).Microseconds()) / 1000
		roots = append(roots, root)
	}
	sortSpans(roots)
	return roots, nil
}

func sortSpans(spans []*TraceSpan) {
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start.Before(spans[j].Start) })
	for _, s := range spans {
		sortSpans(s.Children)
	}
}

func attrMap(attrs []otlpAttr) map[string]string {
	if len(attrs) == 0 {
		return nil
	}
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.Key] = fmt.Sprintf("%v", a.Value.Value)
	}
	return m
}
