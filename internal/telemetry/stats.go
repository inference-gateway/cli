package telemetry

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Stats is the `infer stats` aggregate over the local telemetry files. Empty is
// true when no datapoints matched (clean empty-store render).
type Stats struct {
	Tools    []ToolStat    `json:"tools"`
	Models   []ModelStat   `json:"models"`
	Sessions []SessionStat `json:"sessions"`
	Empty    bool          `json:"-"`
}

// ToolStat aggregates one tool. Failures counts the error outcome (a rejection
// is not a failure). AvgMs is the mean execution duration.
type ToolStat struct {
	Name     string `json:"name"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
	AvgMs    int64  `json:"avg_ms"`
}

// ModelStat aggregates token usage and cost for one model.
type ModelStat struct {
	Model      string  `json:"model"`
	Prompt     int     `json:"prompt"`
	Completion int     `json:"completion"`
	Total      int     `json:"total"`
	Cost       float64 `json:"cost"`
}

// SessionStat counts sessions by execution mode (interactive/headless) and agent mode.
type SessionStat struct {
	Execution string `json:"execution"`
	Mode      string `json:"mode"`
	Count     int    `json:"count"`
}

// Aggregate reads the per-session OTLP/stdout files under dir and folds their
// delta datapoints (timestamped on/after since) into Stats. The JSON shape is
// the SDK stdout exporter's ResourceMetrics; delta temporality means summing
// every datapoint across every file yields the totals.
func Aggregate(dir string, since time.Time) (Stats, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return Stats{}, err
	}

	tools := map[string]*toolAgg{}
	models := map[string]*ModelStat{}
	sessions := map[string]*SessionStat{}
	seen := false

	for _, f := range files {
		if err := foldFile(f, since, tools, models, sessions, &seen); err != nil {
			return Stats{}, err
		}
	}

	if !seen {
		return Stats{Empty: true}, nil
	}
	return Stats{
		Tools:    toolStats(tools),
		Models:   modelStats(models),
		Sessions: sessionStats(sessions),
	}, nil
}

// Archive moves session files older than cutoff into an archive/ subdir instead
// of deleting them; Aggregate's non-recursive glob then skips them. Best-effort.
func Archive(dir string, cutoff time.Time) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	archiveDir := filepath.Join(dir, "archive")
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.MkdirAll(archiveDir, 0o755); err != nil {
			return
		}
		_ = os.Rename(f, filepath.Join(archiveDir, filepath.Base(f)))
	}
}

type toolAgg struct {
	calls    int
	failures int
	durSum   float64 // seconds
	durCount uint64
}

// resourceMetrics is a minimal decode of one stdout-exporter line.
type resourceMetrics struct {
	Resource     []otlpAttr `json:"Resource"`
	ScopeMetrics []struct {
		Metrics []struct {
			Name string `json:"Name"`
			Data struct {
				DataPoints []dataPoint `json:"DataPoints"`
			} `json:"Data"`
		} `json:"Metrics"`
	} `json:"ScopeMetrics"`
}

type dataPoint struct {
	Attributes []otlpAttr `json:"Attributes"`
	Time       time.Time  `json:"Time"`
	Value      float64    `json:"Value"` // counters
	Sum        float64    `json:"Sum"`   // histograms
	Count      uint64     `json:"Count"` // histograms
}

type otlpAttr struct {
	Key   string `json:"Key"`
	Value struct {
		Value any `json:"Value"`
	} `json:"Value"`
}

func attrOf(attrs []otlpAttr, key string) string {
	for _, a := range attrs {
		if a.Key == key {
			if s, ok := a.Value.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func foldFile(path string, since time.Time, tools map[string]*toolAgg, models map[string]*ModelStat, sessions map[string]*SessionStat, seen *bool) error {
	fh, err := os.Open(path)
	if err != nil {
		return nil // best-effort: skip unreadable files
	}
	defer func() { _ = fh.Close() }()

	scanner := bufio.NewScanner(fh)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		var rm resourceMetrics
		if json.Unmarshal(scanner.Bytes(), &rm) != nil {
			continue
		}
		execMode := attrOf(rm.Resource, "infer.execution.mode")
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				for _, dp := range m.Data.DataPoints {
					if !since.IsZero() && dp.Time.Before(since) {
						continue
					}
					foldPoint(m.Name, execMode, dp, tools, models, sessions, seen)
				}
			}
		}
	}
	return scanner.Err()
}

func foldPoint(name, execMode string, dp dataPoint, tools map[string]*toolAgg, models map[string]*ModelStat, sessions map[string]*SessionStat, seen *bool) {
	switch name {
	case "infer.agent.tool.calls":
		*seen = true
		t := getTool(tools, attrOf(dp.Attributes, "gen_ai.tool.name"))
		t.calls += int(dp.Value)
		if attrOf(dp.Attributes, "infer.tool.outcome") == ToolError {
			t.failures += int(dp.Value)
		}
	case "gen_ai.execute_tool.duration":
		*seen = true
		t := getTool(tools, attrOf(dp.Attributes, "gen_ai.tool.name"))
		t.durSum += dp.Sum
		t.durCount += dp.Count
	case "gen_ai.client.token.usage":
		*seen = true
		m := getModel(models, attrOf(dp.Attributes, "gen_ai.request.model"))
		if attrOf(dp.Attributes, "gen_ai.token.type") == "input" {
			m.Prompt += int(dp.Sum)
		} else {
			m.Completion += int(dp.Sum)
		}
	case "infer.client.cost":
		*seen = true
		getModel(models, attrOf(dp.Attributes, "gen_ai.request.model")).Cost += dp.Value
	case "infer.agent.runs":
		*seen = true
		mode := attrOf(dp.Attributes, "infer.agent.mode")
		key := execMode + "\x00" + mode
		s := sessions[key]
		if s == nil {
			s = &SessionStat{Execution: execMode, Mode: mode}
			sessions[key] = s
		}
		s.Count += int(dp.Value)
	}
}

func getTool(tools map[string]*toolAgg, name string) *toolAgg {
	t := tools[name]
	if t == nil {
		t = &toolAgg{}
		tools[name] = t
	}
	return t
}

func getModel(models map[string]*ModelStat, name string) *ModelStat {
	m := models[name]
	if m == nil {
		m = &ModelStat{Model: name}
		models[name] = m
	}
	return m
}

func toolStats(tools map[string]*toolAgg) []ToolStat {
	out := make([]ToolStat, 0, len(tools))
	for name, t := range tools {
		var avg int64
		if t.durCount > 0 {
			avg = int64(t.durSum / float64(t.durCount) * 1000)
		}
		out = append(out, ToolStat{Name: name, Calls: t.calls, Failures: t.failures, AvgMs: avg})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Calls > out[j].Calls })
	return out
}

func modelStats(models map[string]*ModelStat) []ModelStat {
	out := make([]ModelStat, 0, len(models))
	for _, m := range models {
		m.Total = m.Prompt + m.Completion
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

func sessionStats(sessions map[string]*SessionStat) []SessionStat {
	out := make([]SessionStat, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}
