package metrics

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// ToolStat aggregates one tool's calls. Failures counts the ToolError outcome
// (a rejection is not a failure). P50ms/P95ms are nearest-rank percentiles.
type ToolStat struct {
	Name     string `json:"name"`
	Calls    int    `json:"calls"`
	Failures int    `json:"failures"`
	P50ms    int64  `json:"p50_ms"`
	P95ms    int64  `json:"p95_ms"`
}

// ModelStat aggregates token usage and derived cost for one model.
type ModelStat struct {
	Model      string  `json:"model"`
	Prompt     int     `json:"prompt"`
	Completion int     `json:"completion"`
	Total      int     `json:"total"`
	Cost       float64 `json:"cost"`
}

// ModeStat aggregates sessions for one agent mode. Incomplete counts sessions
// that recorded a start but no end (crashed/killed before teardown).
type ModeStat struct {
	Mode       string `json:"mode"`
	Count      int    `json:"count"`
	Incomplete int    `json:"incomplete"`
}

// Stats is the full aggregate rendered by `infer stats`. Empty is true when no
// events matched (so the command can print a clean empty-store message).
type Stats struct {
	Tools  []ToolStat  `json:"tools"`
	Models []ModelStat `json:"models"`
	Modes  []ModeStat  `json:"modes"`
	Empty  bool        `json:"-"`
}

// CostFunc returns the total cost for a model's token counts. Pass nil to leave
// the cost column zero. Wraps domain.PricingService.CalculateCost's total return.
type CostFunc func(model string, prompt, completion int) float64

// Aggregate loads every event under dir with timestamp on/after since (zero
// since = all time) and folds them into Stats.
func Aggregate(dir string, since time.Time, cost CostFunc) (Stats, error) {
	events, err := load(dir, since)
	if err != nil {
		return Stats{}, err
	}
	if len(events) == 0 {
		return Stats{Empty: true}, nil
	}

	tools := map[string]*toolAcc{}
	models := map[string]*ModelStat{}
	sessions := map[string]*sessionAcc{}

	for _, e := range events {
		switch e.Kind {
		case KindTool:
			acc := tools[e.Tool]
			if acc == nil {
				acc = &toolAcc{}
				tools[e.Tool] = acc
			}
			acc.calls++
			if e.Outcome == ToolError {
				acc.failures++
			}
			acc.durs = append(acc.durs, e.DurMs)
		case KindUsage:
			m := models[e.Model]
			if m == nil {
				m = &ModelStat{Model: e.Model}
				models[e.Model] = m
			}
			m.Prompt += e.Prompt
			m.Completion += e.Completion
			m.Total += e.Prompt + e.Completion
		case KindSession:
			s := sessions[e.Session]
			if s == nil {
				s = &sessionAcc{}
				sessions[e.Session] = s
			}
			if e.Mode != "" {
				s.mode = e.Mode // end phase wins (it's recorded last)
			}
			if e.Phase == phaseEnd {
				s.ended = true
			}
		}
	}

	return Stats{
		Tools:  toolStats(tools),
		Models: modelStats(models, cost),
		Modes:  modeStats(sessions),
	}, nil
}

// Prune removes whole month-files whose month ended before cutoff. Best-effort:
// unparseable names and remove errors are ignored.
func Prune(dir string, cutoff time.Time) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for _, f := range files {
		month, err := time.Parse("2006-01", strings.TrimSuffix(filepath.Base(f), ".jsonl"))
		if err != nil {
			continue
		}
		if month.AddDate(0, 1, 0).Before(cutoff) { // end of that month
			_ = os.Remove(f)
		}
	}
}

type toolAcc struct {
	calls    int
	failures int
	durs     []int64
}

type sessionAcc struct {
	mode  string
	ended bool
}

func toolStats(tools map[string]*toolAcc) []ToolStat {
	out := make([]ToolStat, 0, len(tools))
	for name, acc := range tools {
		out = append(out, ToolStat{
			Name:     name,
			Calls:    acc.calls,
			Failures: acc.failures,
			P50ms:    percentile(acc.durs, 50),
			P95ms:    percentile(acc.durs, 95),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Calls > out[j].Calls })
	return out
}

func modelStats(models map[string]*ModelStat, cost CostFunc) []ModelStat {
	out := make([]ModelStat, 0, len(models))
	for _, m := range models {
		if cost != nil {
			m.Cost = cost(m.Model, m.Prompt, m.Completion)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Total > out[j].Total })
	return out
}

func modeStats(sessions map[string]*sessionAcc) []ModeStat {
	byMode := map[string]*ModeStat{}
	for _, s := range sessions {
		mode := s.mode
		if mode == "" {
			mode = "unknown"
		}
		ms := byMode[mode]
		if ms == nil {
			ms = &ModeStat{Mode: mode}
			byMode[mode] = ms
		}
		ms.Count++
		if !s.ended {
			ms.Incomplete++
		}
	}
	out := make([]ModeStat, 0, len(byMode))
	for _, ms := range byMode {
		out = append(out, *ms)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	return out
}

// percentile returns the nearest-rank pth percentile (in ms) of durs.
func percentile(durs []int64, p int) int64 {
	if len(durs) == 0 {
		return 0
	}
	s := slices.Clone(durs)
	slices.Sort(s)
	idx := (p*len(s)+99)/100 - 1 // ceil(p/100 * N) - 1
	idx = max(idx, 0)
	idx = min(idx, len(s)-1)
	return s[idx]
}

// load reads and parses every *.jsonl file under dir, keeping events on/after
// since. Best-effort: unreadable files and malformed lines are skipped.
func load(dir string, since time.Time) ([]event, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	var events []event
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(fh)
		scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			var e event
			if json.Unmarshal(scanner.Bytes(), &e) != nil {
				continue
			}
			if !since.IsZero() && e.Time.Before(since) {
				continue
			}
			events = append(events, e)
		}
		scanErr := scanner.Err()
		_ = fh.Close()
		if scanErr != nil {
			return events, fmt.Errorf("reading %s: %w", f, scanErr)
		}
	}
	return events, nil
}
