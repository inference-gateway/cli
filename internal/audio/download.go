package audio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

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

	written, err := io.Copy(tmp, resp.Body)
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
