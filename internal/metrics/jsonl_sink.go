package metrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// jsonlSink appends events as one JSON line each to a monthly-rotated file under
// dir. O_APPEND makes each sub-PIPE_BUF line atomic across goroutines AND across
// processes (chat + the headless `infer agent` subprocess share the file), so no
// mutex and no long-lived handle. The directory is created lazily on first write.
//
// ponytail: O_APPEND atomicity is the whole concurrency story; switch to a
// buffered handle + mutex only if event volume ever spikes past that.
type jsonlSink struct {
	dir string
}

func (s *jsonlSink) record(e event) {
	line, err := json.Marshal(e)
	if err != nil {
		return
	}
	line = append(line, '\n')

	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return // best-effort: metrics never break a run
	}
	f, err := os.OpenFile(s.filePath(e.Time), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.Write(line)
}

func (s *jsonlSink) shutdown(context.Context) error { return nil }

func (s *jsonlSink) filePath(t time.Time) string {
	return filepath.Join(s.dir, t.Format("2006-01")+".jsonl")
}
