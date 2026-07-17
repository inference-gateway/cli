package stt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// sttBinariesBase is the release hosting prebuilt static STT binaries
// (whisper-cli, ffmpeg), published as <base>/<name>-<GOOS>-<GOARCH> plus a
// checksums.txt with sha256 sums. Releases are immutable; bump the tag here
// to adopt a newer stt-binaries release.
const sttBinariesBase = "https://github.com/inference-gateway/stt-binaries/releases/download/v0.1.0"

// BinaryManager downloads prebuilt STT helper binaries (whisper-cli, ffmpeg)
// into ~/.infer/bin on demand, mirroring ModelManager for GGML models.
type BinaryManager struct {
	cfg config.SpeechToTextConfig

	// baseURL and client are overridable in tests.
	baseURL string
	client  *http.Client
}

// NewBinaryManager creates a BinaryManager from the speech-to-text config.
func NewBinaryManager(cfg config.SpeechToTextConfig) *BinaryManager {
	return &BinaryManager{
		cfg:     cfg,
		baseURL: sttBinariesBase,
		client:  http.DefaultClient,
	}
}

// binDir returns the userspace binary directory ~/.infer/bin.
func binDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, config.ConfigDirName, "bin"), nil
}

// assetName returns the release asset name for a binary on this platform.
func assetName(name string) string {
	return fmt.Sprintf("%s-%s-%s", name, runtime.GOOS, runtime.GOARCH)
}

// EnsureBinary returns the local path to the named binary under ~/.infer/bin,
// downloading it (checksum-verified) on first use when auto_download is
// enabled. An existing file is returned as-is.
func (b *BinaryManager) EnsureBinary(ctx context.Context, name string) (string, error) {
	dir, err := binDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)

	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return path, nil
	}

	if !b.cfg.AutoDownload {
		return "", fmt.Errorf("%s not found at %s and speech_to_text.auto_download is disabled", name, path)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating bin directory: %w", err)
	}

	asset := assetName(name)
	sum, err := b.fetchChecksum(ctx, asset)
	if err != nil {
		return "", err
	}

	if err := b.download(ctx, b.baseURL+"/"+asset, path, sum); err != nil {
		return "", err
	}
	return path, nil
}

// fetchChecksum returns the expected sha256 for asset from the release's
// checksums.txt ("<hex>  <asset>" per line). A missing entry means the
// platform has no prebuilt binary.
func (b *BinaryManager) fetchChecksum(ctx context.Context, asset string) (string, error) {
	body, err := b.get(ctx, b.baseURL+"/checksums.txt")
	if err != nil {
		return "", fmt.Errorf("fetching STT binary checksums: %w", err)
	}
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading STT binary checksums: %w", err)
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("no prebuilt %s available for %s/%s: install it manually or set the binary path in config", asset, runtime.GOOS, runtime.GOARCH)
}

// download fetches url into dstPath atomically (temp file + rename), verifying
// the sha256 checksum before the file becomes visible, and marks it executable.
func (b *BinaryManager) download(ctx context.Context, url, dstPath, wantSum string) error {
	body, err := b.get(ctx, url)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", filepath.Base(dstPath), err)
	}
	defer func() { _ = body.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(dstPath), "."+filepath.Base(dstPath)+"-*.partial")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing %s: %w", filepath.Base(dstPath), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", filepath.Base(dstPath), err)
	}

	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, wantSum) {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", filepath.Base(dstPath), got, wantSum)
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return fmt.Errorf("marking %s executable: %w", filepath.Base(dstPath), err)
	}
	if err := os.Rename(tmpName, dstPath); err != nil {
		return fmt.Errorf("finalizing %s: %w", filepath.Base(dstPath), err)
	}
	return nil
}

// get issues a GET and returns the body, following GitHub release redirects.
func (b *BinaryManager) get(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("status %d from %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}
