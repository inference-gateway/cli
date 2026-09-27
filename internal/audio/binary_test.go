package audio

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
	"strconv"
	"strings"
	"sync"
	"testing"

	config "github.com/inference-gateway/cli/config"
)

// resetVerified clears the package-level process cache so tests stay isolated.
func resetVerified(t *testing.T) {
	t.Helper()
	verified = sync.Map{}
	t.Cleanup(func() { verified = sync.Map{} })
}

// testBinaryStore returns a store whose tools dir is redirected via HOME and
// whose baseURL/installerURL point at srv.
func testBinaryStore(t *testing.T, autoDownload bool, srv *httptest.Server) *BinaryStore {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	resetVerified(t)
	m := NewBinaryStore(config.SpeechToTextConfig{AutoDownload: autoDownload})
	if srv != nil {
		m.baseURL = srv.URL
		m.installerURL = srv.URL + "/install.sh"
	}
	return m
}

// binaryServer serves checksums.txt covering all default binaries and the
// platform asset for content, plus a stub install.sh that creates the install
// dir and writes content into $INSTALL_DIR/$1 (counting its own fetches when
// the counter is non-nil), simulating a successful install.
func binaryServer(t *testing.T, name, content string, installerFetches *int) *httptest.Server {
	t.Helper()
	asset := assetName(name)
	sum := sha256hex(content)
	script := "#!/bin/sh\nmkdir -p \"$INSTALL_DIR\"\nprintf '%s' " + strconv.Quote(content) + " > \"$INSTALL_DIR/$1\"\nchmod +x \"$INSTALL_DIR/$1\"\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = fmt.Fprintf(w, "%s  %s\n", sum, assetName("whisper-cli"))
			_, _ = fmt.Fprintf(w, "%s  %s\n", sum, assetName("ffmpeg"))
			_, _ = fmt.Fprintf(w, "%s  %s\n", sum, assetName("llama-tts"))
		case strings.HasSuffix(r.URL.Path, "/"+asset):
			_, _ = w.Write([]byte(content))
		case strings.HasSuffix(r.URL.Path, "/install.sh"):
			if installerFetches != nil {
				*installerFetches++
			}
			_, _ = w.Write([]byte(script))
		default:
			http.NotFound(w, r)
		}
	}))
}

// unsupportedPlatformServer offers checksums for an unrelated platform only;
// its install.sh stub exits non-zero like the real one does for a missing asset.
func unsupportedPlatformServer() *httptest.Server {
	script := "#!/bin/sh\necho \"no prebuilt asset in the release\" >&2\nexit 1\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = fmt.Fprintln(w, "abc  whisper-cli-plan9-mips")
		case strings.HasSuffix(r.URL.Path, "/install.sh"):
			_, _ = w.Write([]byte(script))
		default:
			http.NotFound(w, r)
		}
	}))
}

// toolsPath returns the tools-dir path for a binary under the redirected HOME.
func toolsPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := toolsBinDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, name+exeSuffix())
}

func writeBinary(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestEnsureBinaryInstalls(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", nil)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	if path != toolsPath(t, "whisper-cli") {
		t.Errorf("EnsureBinary = %q, want the tools path %q", path, toolsPath(t, "whisper-cli"))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if string(data) != "#!fake-binary" {
		t.Errorf("installed binary content = %q", data)
	}
	if runtime.GOOS != "windows" {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat installed binary: %v", err)
		}
		if fi.Mode().Perm()&0o111 == 0 {
			t.Errorf("installed binary is not executable: %v", fi.Mode())
		}
	}
}

func TestEnsureBinaryCurrentSkipped(t *testing.T) {
	var installerFetches int
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", &installerFetches)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	if err := os.MkdirAll(filepath.Dir(toolsPath(t, "whisper-cli")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolsPath(t, "whisper-cli"), []byte("#!fake-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	if path != toolsPath(t, "whisper-cli") {
		t.Errorf("EnsureBinary = %q, want cached %q", path, toolsPath(t, "whisper-cli"))
	}
	if installerFetches != 0 {
		t.Errorf("install.sh fetched %d times for a current binary, want 0", installerFetches)
	}
}

func TestEnsureBinaryStaleReplaced(t *testing.T) {
	var installerFetches int
	srv := binaryServer(t, "whisper-cli", "#!fake-binary-v2", &installerFetches)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	writeBinary(t, toolsPath(t, "whisper-cli"), "#!fake-binary-v1")
	legacy, err := legacyBinDir()
	if err != nil {
		t.Fatal(err)
	}
	writeBinary(t, filepath.Join(legacy, "whisper-cli"), "#!fake-binary-v1")

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(data) != "#!fake-binary-v2" {
		t.Errorf("stale binary was not replaced; content = %q", data)
	}
	if installerFetches != 1 {
		t.Errorf("install.sh fetched %d times for a stale binary, want 1", installerFetches)
	}
	if _, err := os.Stat(filepath.Join(legacy, "whisper-cli")); !os.IsNotExist(err) {
		t.Errorf("legacy ~/.infer/bin/whisper-cli still exists: %v", err)
	}
}

func TestEnsureBinaryStaleReplacedNative(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary-v2", nil)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	m.shLookup = func() (string, error) { return "", fmt.Errorf("no POSIX shell") }
	writeBinary(t, toolsPath(t, "whisper-cli"), "#!fake-binary-v1")

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if string(data) != "#!fake-binary-v2" {
		t.Errorf("native fallback did not replace the stale binary; content = %q", data)
	}
}

func TestEnsureBinaryOfflineKeepsExisting(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", nil)
	m := testBinaryStore(t, true, srv)
	existing := toolsPath(t, "whisper-cli")
	writeBinary(t, existing, "#!local-binary")
	srv.Close()

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary kept the binary offline: %v", err)
	}
	if path != existing {
		t.Errorf("EnsureBinary = %q, want the existing %q", path, existing)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "#!local-binary" {
		t.Errorf("existing binary was modified offline: %v %q", err, data)
	}
}

func TestEnsureBinaryChecksOncePerProcess(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", nil)
	m := testBinaryStore(t, true, srv)
	writeBinary(t, toolsPath(t, "whisper-cli"), "#!fake-binary")

	if _, err := m.EnsureBinary(context.Background(), "whisper-cli"); err != nil {
		t.Fatalf("first EnsureBinary: %v", err)
	}
	srv.Close()

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("second EnsureBinary hit the (closed) release despite the per-process check: %v", err)
	}
	if path != toolsPath(t, "whisper-cli") {
		t.Errorf("EnsureBinary = %q, want %q", path, toolsPath(t, "whisper-cli"))
	}
}

func TestEnsureBinaryStaleKeptWhenAutoDownloadDisabled(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary-v2", nil)
	defer srv.Close()

	m := testBinaryStore(t, false, srv)
	existing := toolsPath(t, "whisper-cli")
	writeBinary(t, existing, "#!fake-binary-v1")

	path, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err != nil {
		t.Fatalf("EnsureBinary: %v", err)
	}
	if path != existing {
		t.Errorf("EnsureBinary = %q, want the existing %q", path, existing)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "#!fake-binary-v1" {
		t.Errorf("auto_download disabled must never touch the binary: %v %q", err, data)
	}
}

func TestEnsureBinaryAutoDownloadDisabled(t *testing.T) {
	m := testBinaryStore(t, false, nil)
	_, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err == nil || !strings.Contains(err.Error(), "auto_download is disabled") {
		t.Fatalf("expected auto_download disabled error, got %v", err)
	}
}

func TestEnsureBinaryUnsupportedPlatform(t *testing.T) {
	srv := unsupportedPlatformServer()
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	_, err := m.EnsureBinary(context.Background(), "whisper-cli")
	if err == nil || !strings.Contains(err.Error(), "install.sh failed") {
		t.Fatalf("expected the installer's no-asset error, got %v", err)
	}
}

func TestStatusStates(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", nil)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	writeBinary(t, toolsPath(t, "whisper-cli"), "#!fake-binary")
	writeBinary(t, toolsPath(t, "ffmpeg"), "#!stale-ffmpeg")

	statuses, err := m.Status(context.Background(), nil)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	want := map[string]BinaryState{
		"whisper-cli": BinaryCurrent,
		"ffmpeg":      BinaryStale,
		"llama-tts":   BinaryMissing,
	}
	if len(statuses) != len(want) {
		t.Fatalf("Status returned %d entries, want %d", len(statuses), len(want))
	}
	for _, st := range statuses {
		if st.State != want[st.Name] {
			t.Errorf("%s state = %q, want %q", st.Name, st.State, want[st.Name])
		}
	}
}

func TestStatusUnknownName(t *testing.T) {
	srv := binaryServer(t, "whisper-cli", "#!fake-binary", nil)
	defer srv.Close()

	m := testBinaryStore(t, true, srv)
	statuses, err := m.Status(context.Background(), []string{"not-a-tool"})
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	st := statuses[0]
	if st.State != BinaryMissing {
		t.Errorf("unknown name state = %q, want missing", st.State)
	}
	if !strings.Contains(st.Detail, "no prebuilt") {
		t.Errorf("unknown name detail = %q, want a no-prebuilt note", st.Detail)
	}
}
