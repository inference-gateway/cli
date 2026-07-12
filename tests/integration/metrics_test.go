package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	metrics "github.com/inference-gateway/cli/internal/metrics"
)

// metricsDir is the recorder's output directory relative to the current (per
// sub-test temp) working directory: newEnv chdir's there and metrics.enabled
// defaults on, so the container writes events under <cwd>/.infer/metrics.
func metricsDir() string {
	return filepath.Join(config.DefaultConfig().GetConfigDir(), "metrics")
}

// TestMetricsRecorderEndToEnd is the acceptance check for issue #841: driving the
// agent against the mock records tool outcomes and token usage that the `infer
// stats` aggregation reflects - a failing tool shows as one failure, and the
// usage-report scenario's 142 tokens are counted. The two facets run in separate
// sub-tests (hence separate temp metrics dirs) so token polyfill on the
// failing-tool run's usage-less turns can't pollute the exact 142-token count.
func TestMetricsRecorderEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	t.Run("failing tool is recorded as one failure", func(t *testing.T) {
		e := newEnv(t)

		res := e.runStream(ctx, t, "read the missing file")
		require.Empty(t, res.errs, "a failing tool must complete, not surface as a stream error")

		stats, err := metrics.Aggregate(metricsDir(), time.Time{}, nil)
		require.NoError(t, err)
		require.False(t, stats.Empty)

		read := findToolStat(stats.Tools, "Read")
		require.NotNil(t, read, "the Read call must be recorded")
		require.Equal(t, 1, read.Failures, "the missing-file Read must count as one failure")
		require.Equal(t, 1, totalFailures(stats.Tools), "exactly one tool failure across the run")
	})

	t.Run("usage-report tokens are counted", func(t *testing.T) {
		e := newEnv(t)

		res := e.runStream(ctx, t, "report your usage")
		require.Empty(t, res.errs)

		stats, err := metrics.Aggregate(metricsDir(), time.Time{}, nil)
		require.NoError(t, err)
		require.Len(t, stats.Models, 1, "one model recorded usage")
		require.Equal(t, 142, stats.Models[0].Total, "100 prompt + 42 completion tokens")
		require.Equal(t, 100, stats.Models[0].Prompt)
		require.Equal(t, 42, stats.Models[0].Completion)
	})
}

func findToolStat(tools []metrics.ToolStat, name string) *metrics.ToolStat {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func totalFailures(tools []metrics.ToolStat) int {
	n := 0
	for _, s := range tools {
		n += s.Failures
	}
	return n
}
