package infrastructure

import (
	"encoding/base64"
	"fmt"
	"os"
	"testing"
	"time"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

func testFrame(id string) *agentdomain.Frame {
	return &agentdomain.Frame{
		ID:        id,
		Timestamp: time.Now(),
		Data:      base64.StdEncoding.EncodeToString([]byte("img-" + id)),
		Width:     1024,
		Height:    768,
		Format:    "png",
	}
}

func newTestBuffer(t *testing.T, maxSize int) *CircularScreenshotBuffer {
	t.Helper()
	buf, err := NewCircularScreenshotBuffer(maxSize, t.TempDir(), "test")
	require.NoError(t, err)
	return buf
}

func TestCircularScreenshotBufferAddDefaults(t *testing.T) {
	buf := newTestBuffer(t, 3)

	frame := &agentdomain.Frame{Data: base64.StdEncoding.EncodeToString([]byte("img")), Format: "png"}
	require.NoError(t, buf.Add(frame))
	assert.NotEmpty(t, frame.ID, "empty ID must be defaulted to a UUID")
	assert.False(t, frame.Timestamp.IsZero(), "zero timestamp must be defaulted to now")
	assert.FileExists(t, frame.Path, "frame with data must be persisted to disk")

	explicit := testFrame("explicit")
	ts := explicit.Timestamp
	require.NoError(t, buf.Add(explicit))
	assert.Equal(t, "explicit", explicit.ID)
	assert.Equal(t, ts, explicit.Timestamp)
}

func TestCircularScreenshotBufferAddSkipsDiskWithoutData(t *testing.T) {
	buf := newTestBuffer(t, 2)
	frame := &agentdomain.Frame{ID: "nodata"}
	require.NoError(t, buf.Add(frame))
	assert.Empty(t, frame.Path)
	assert.Equal(t, 1, buf.Count())
}

func TestCircularScreenshotBufferWrapAround(t *testing.T) {
	tests := []struct {
		name           string
		maxSize        int
		adds           int
		wantCount      int
		wantRecentIDs  []string // recent-first
		wantEvictedIDs []string
	}{
		{"under capacity", 3, 2, 2, []string{"s2", "s1"}, nil},
		{"exactly full", 3, 3, 3, []string{"s3", "s2", "s1"}, nil},
		{"one past capacity", 3, 4, 3, []string{"s4", "s3", "s2"}, []string{"s1"}},
		{"wraps past capacity", 3, 5, 3, []string{"s5", "s4", "s3"}, []string{"s1", "s2"}},
		{"single slot", 1, 3, 1, []string{"s3"}, []string{"s1", "s2"}},
		{"two full laps", 2, 6, 2, []string{"s6", "s5"}, []string{"s1", "s2", "s3", "s4"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := newTestBuffer(t, tt.maxSize)
			paths := make(map[string]string, tt.adds)
			for i := 1; i <= tt.adds; i++ {
				frame := testFrame(fmt.Sprintf("s%d", i))
				require.NoError(t, buf.Add(frame))
				paths[frame.ID] = frame.Path
			}

			assert.Equal(t, tt.wantCount, buf.Count())

			latest, err := buf.GetLatest()
			require.NoError(t, err)
			assert.Equal(t, tt.wantRecentIDs[0], latest.ID)

			recent := buf.GetRecent(0)
			ids := make([]string, len(recent))
			for i, f := range recent {
				ids[i] = f.ID
			}
			assert.Equal(t, tt.wantRecentIDs, ids)

			for _, id := range tt.wantRecentIDs {
				got, err := buf.GetByID(id)
				require.NoError(t, err, "retained screenshot %s", id)
				assert.Equal(t, id, got.ID)
				assert.FileExists(t, paths[id], "retained screenshot must keep its disk file")
			}
			for _, id := range tt.wantEvictedIDs {
				_, err := buf.GetByID(id)
				assert.Error(t, err, "evicted screenshot %s must not be findable", id)
				assert.NoFileExists(t, paths[id], "evicted screenshot must be removed from disk")
			}
		})
	}
}

func TestCircularScreenshotBufferGetLatestEmpty(t *testing.T) {
	buf := newTestBuffer(t, 3)
	_, err := buf.GetLatest()
	assert.ErrorContains(t, err, "buffer is empty")
}

func TestCircularScreenshotBufferGetRecentLimits(t *testing.T) {
	buf := newTestBuffer(t, 3)
	for i := 1; i <= 5; i++ {
		require.NoError(t, buf.Add(testFrame(fmt.Sprintf("s%d", i))))
	}

	tests := []struct {
		name    string
		limit   int
		wantIDs []string
	}{
		{"zero returns all", 0, []string{"s5", "s4", "s3"}},
		{"negative returns all", -1, []string{"s5", "s4", "s3"}},
		{"limit above count clamps", 10, []string{"s5", "s4", "s3"}},
		{"partial across wrap boundary", 2, []string{"s5", "s4"}},
		{"single", 1, []string{"s5"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recent := buf.GetRecent(tt.limit)
			ids := make([]string, len(recent))
			for i, f := range recent {
				ids[i] = f.ID
			}
			assert.Equal(t, tt.wantIDs, ids)
		})
	}
}

func TestCircularScreenshotBufferClear(t *testing.T) {
	buf := newTestBuffer(t, 3)
	var paths []string
	for i := 1; i <= 3; i++ {
		frame := testFrame(fmt.Sprintf("s%d", i))
		require.NoError(t, buf.Add(frame))
		paths = append(paths, frame.Path)
	}

	require.NoError(t, buf.Clear())
	assert.Equal(t, 0, buf.Count())
	_, err := buf.GetLatest()
	assert.Error(t, err)
	for _, p := range paths {
		assert.NoFileExists(t, p)
	}

	// buffer stays usable after Clear
	require.NoError(t, buf.Add(testFrame("after")))
	assert.Equal(t, 1, buf.Count())
	latest, err := buf.GetLatest()
	require.NoError(t, err)
	assert.Equal(t, "after", latest.ID)
}

func TestCircularScreenshotBufferCleanup(t *testing.T) {
	tempDir := t.TempDir()
	buf, err := NewCircularScreenshotBuffer(2, tempDir, "sess")
	require.NoError(t, err)
	frame := testFrame("s1")
	require.NoError(t, buf.Add(frame))
	require.FileExists(t, frame.Path)

	require.NoError(t, buf.Cleanup())
	_, statErr := os.Stat(buf.tempDir)
	assert.True(t, os.IsNotExist(statErr), "session temp dir must be removed")
}
