package telemetry

import "testing"

func TestFormatAvg(t *testing.T) {
	tests := []struct {
		name   string
		avgMs  float64
		expect string
	}{
		{"zero", 0, "0µs"},
		{"sub-millisecond", 0.432, "432µs"},
		{"just under one millisecond", 0.999, "999µs"},
		{"one millisecond", 1, "1ms"},
		{"milliseconds", 47.3, "47ms"},
		{"large", 1250.6, "1251ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatAvg(tt.avgMs); got != tt.expect {
				t.Errorf("FormatAvg(%v) = %q, want %q", tt.avgMs, got, tt.expect)
			}
		})
	}
}

// TestToolStatsKeepsSubMsPrecision guards the issue's regression: a sub-millisecond
// mean used to truncate to a whole-millisecond int64 and rendered as 0ms.
func TestToolStatsKeepsSubMsPrecision(t *testing.T) {
	agg := map[string]*toolAgg{"Grep": {calls: 2, durSum: 0.0008, durCount: 2}}
	got := toolStats(agg)
	if got[0].AvgMs != 0.4 {
		t.Errorf("expected 0.4ms avg, got %v", got[0].AvgMs)
	}
}
