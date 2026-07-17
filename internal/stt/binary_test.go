package stt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// testBinaryManager returns a manager whose binDir is redirected via HOME and
// whose baseURL points at srv.
func testBinaryManager(t *testing.T, autoDownload bool, srv *httptest.Server) *BinaryManager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	m := NewBinaryManager(config.SpeechToTextConfig{AutoDownload: autoDownload})
	if srv != nil {
		m.baseURL = srv.URL
	}
	return m
}

func binaryServer(t *testing.T, content string, sum string) *httptest.Server {
	t.Helper()
	asset := assetName("whisper-cli")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", sum, asset)
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write([]byte(content))
		default:
			http.NotFound(w, r)
		}
	}))
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestEnsureBinaryDownloads(t *testing.T) {
	srv := binaryServer(t, "#!fake-binary", sha256hex("#!fake-binary"))
	defer srv.Close()

	m := testBinaryManager(t, true, srv)
	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat downloaded binary: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("downloaded binary is not executable: %v", fi.Mode())
	}
}

func TestEnsureBinaryCachedHit(t *testing.T) {
	m := testBinaryManager(t, false, nil)
	dir, err := binDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "whisper-cli")
	if err := os.WriteFile(existing, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	if path != existing {
		t.Errorf("EnsureBinary = %q, want cached %q", path, existing)
	}
}

func TestEnsureBinaryChecksumMismatch(t *testing.T) {
	srv := binaryServer(t, "#!fake-binary", sha256hex("something else"))
	defer srv.Close()

	m := testBinaryManager(t, true, srv)
	_, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch error, got %v", err)
	}
}

func TestEnsureBinaryAutoDownloadDisabled(t *testing.T) {
	m := testBinaryManager(t, false, nil)
	_, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err == nil || !strings.Contains(err.Error(), "auto_download is disabled") {
		t.Fatalf("expected auto_download disabled error, got %v", err)
	}
}

func TestEnsureBinaryUnsupportedPlatform(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/checksums.txt") {
			_, _ = fmt.Fprintln(w, "abc  whisper-cli-plan9-mips")
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	m := testBinaryManager(t, true, srv)
	_, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err == nil || !strings.Contains(err.Error(), "no prebuilt") {
		t.Fatalf("expected 'no prebuilt' error, got %v", err)
	}
}
