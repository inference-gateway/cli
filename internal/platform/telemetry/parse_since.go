package telemetry

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseSince converts a window like "7d"/"24h"/"30m" to an absolute cutoff.
// Empty means all time (zero cutoff). time.ParseDuration has no day unit, so
// "Nd" is handled here.
func ParseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid --since %q", s)
		}
		return time.Now().AddDate(0, 0, -n), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid --since %q: %w", s, err)
	}
	return time.Now().Add(-d), nil
}
