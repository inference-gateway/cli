package integration

import (
	"context"
	"testing"
	"time"

	require "github.com/stretchr/testify/require"

	config "github.com/inference-gateway/cli/config"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
)

// telemetryDir is the recorder's output directory: telemetry is userspace-global
// (~/.infer/telemetry), and newEnv points $HOME at a per-sub-test temp dir, so
// config.TelemetryDir() resolves under it - matching where the container writes.
func telemetryDir() string {
	return config.TelemetryDir()
}

// TestTelemetryRecorderEndToEnd is the acceptance check: driving the agent
// against the mock records tool outcomes and token usage that the `infer stats`
// aggregation reflects - a failing tool shows as one failure, and the
// usage-report scenario's 142 tokens are counted. The recorder buffers into OTel
// instruments and flushes to the local file, so each sub-test forces a flush
// before aggregating. Separate sub-tests (separate temp dirs) keep the exact
// 142-token count clean from the failing run's polyfilled usage.
func TestTelemetryRecorderEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()

	t.Run("failing tool is recorded as one failure", func(t *testing.T) {
		e := newEnv(t)

		res := e.runStream(ctx, t, "read the missing file")
		require.Empty(t, res.errs, "a failing tool must complete, not surface as a stream error")

		e.container.GetTelemetryRecorder().Flush(ctx)
		stats, err := telemetry.Aggregate(telemetryDir(), time.Time{})
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

		e.container.GetTelemetryRecorder().Flush(ctx)
		stats, err := telemetry.Aggregate(telemetryDir(), time.Time{})
		require.NoError(t, err)
		require.Len(t, stats.Models, 1, "one model recorded usage")
		require.Equal(t, 142, stats.Models[0].Total, "100 prompt + 42 completion tokens")
		require.Equal(t, 100, stats.Models[0].Prompt)
		require.Equal(t, 42, stats.Models[0].Completion)
	})
}

func findToolStat(tools []telemetry.ToolStat, name string) *telemetry.ToolStat {
	for i := range tools {
		if tools[i].Name == name {
			return &tools[i]
		}
	}
	return nil
}

func totalFailures(tools []telemetry.ToolStat) int {
	n := 0
	for _, s := range tools {
		n += s.Failures
	}
	return n
}
