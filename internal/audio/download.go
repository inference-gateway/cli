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

// progressReader reports how far a transfer has got through the context's tool
// progress callback. Models here run to hundreds of megabytes, so without this
// the UI sits on a motionless "Executing..." for minutes. Reporting is
// throttled to once a second so a chunked body cannot flood the event stream.
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
	// The zero next time makes the first read report immediately, so the user
	// sees the download start rather than a second of silence.
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

// downloadToFile fetches url into dstPath atomically (temp file + rename, so an
// interrupted download never leaves a half-written model at the final path) and
// verifies the received byte count against the response Content-Length, so a
// silently truncated response is never cached either. label names the model
// kind in error messages ("whisper model", "tts model").
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

// cachedModelStale reports whether an existing cache entry should be discarded
// and re-fetched: it HEADs the download URL and compares the local size with
// the server's Content-Length, so a truncated model (interrupted manual fetch,
// early-close response, partial copy from an older tool) is re-downloaded
// instead of failing synthesis on every run. Unknown sizes - probe outage,
// non-200, missing Content-Length - always trust the cache, so an offline
// machine keeps working from its cache. auto_download is false only for
// manually managed models, which are never re-fetched behind the user's back.
//
// ponytail: size comparison rather than a content sha256 (HF's tree API exposes
// lfs.oid if a real checksum is ever needed) - same-size corruption of a GGUF
// is not detected here; llama.cpp still rejects malformed models.
func cachedModelStale(ctx context.Context, client *http.Client, url, path string, autoDownload bool) bool {
	if !autoDownload {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return false
	}
	return info.Size() != resp.ContentLength
}
