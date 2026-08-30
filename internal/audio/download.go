package audio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// progressReader reports transfer progress through the context callback,
// throttled to once per second.
type progressReader struct {
	src    io.Reader
	report agentdomain.ToolProgressCallback
	label  string
	total  int64
	read   int64
	next   time.Time
}

// newProgressReader wraps src only when the context carries a callback, so the
// no-listener case (headless, tests) stays a plain read with no bookkeeping.
func newProgressReader(ctx context.Context, src io.Reader, label string, total int64) io.Reader {
	report := agentdomain.GetToolProgressCallback(ctx)
	if report == nil {
		return src
	}
	return &progressReader{src: src, report: report, label: label, total: total}
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.src.Read(b)
	p.read += int64(n)
	if now := time.Now(); now.After(p.next) {
		p.next = now.Add(time.Second)
		p.report(p.message())
	}
	return n, err
}

// message degrades to a bare byte count when the server sent no Content-Length,
// which is the only case where a percentage would be a lie.
func (p *progressReader) message() string {
	if p.total <= 0 {
		return fmt.Sprintf("Downloading %s: %s", p.label, megabytes(p.read))
	}
	return fmt.Sprintf("Downloading %s: %s of %s (%d%%)",
		p.label, megabytes(p.read), megabytes(p.total), p.read*100/p.total)
}

func megabytes(n int64) string {
	return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
}

// downloadToFile atomically fetches url into dstPath and rejects transfers
// shorter than their declared Content-Length.
func downloadToFile(ctx context.Context, client *http.Client, url, dstPath, label string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", label, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: status %d from %s", label, resp.StatusCode, url)
	}

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+".*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	written, err := io.Copy(tmp, newProgressReader(ctx, resp.Body, label, resp.ContentLength))
	if err == nil && resp.ContentLength > 0 && written != resp.ContentLength {
		err = fmt.Errorf("incomplete download: got %d of %d bytes", written, resp.ContentLength)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", label, err)
	}

	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing %s: %w", label, err)
	}
	return nil
}
