package infrastructure

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	gatewaydomain "github.com/inference-gateway/cli/internal/gateway/domain"
)

const (
	testGatewayBinaryName = "inference-gateway"
	testOldGatewayBinary  = "old gateway"
	testNewGatewayBinary  = "new gateway"
	testOtherArchiveFile  = "README.md"
	testSelfKillingBinary = "#!/bin/sh\nkill -KILL $$\n"
	testVersionedBinary   = "#!/bin/sh\necho inference-gateway version " + testLatestTag + "\n"
	testFailingBinary     = "#!/bin/sh\nexit 1\n"
	testSilentBinary      = "#!/bin/sh\n"
	testExecutableBits    = 0o111
	testFilesAfterUpdate  = 1
	testLatestTag         = "v0.65.0"
	testLatestVersion     = "0.65.0"
)

func gatewayReleaseArchive(t *testing.T, files map[string]string) *bytes.Buffer {
	t.Helper()
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		header := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: gatewayBinaryMode, Size: int64(len(content))}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return &archive
}

// An update must land in a new file: macOS ends a signed binary whose file was
// rewritten in place every time it starts.
func TestExtractGatewayTarGzReplacesTheBinaryWithANewFile(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, testGatewayBinaryName)
	if err := os.WriteFile(dest, []byte(testOldGatewayBinary), gatewayBinaryMode); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}

	archive := gatewayReleaseArchive(t, map[string]string{
		testOtherArchiveFile:  testOldGatewayBinary,
		testGatewayBinaryName: testNewGatewayBinary,
	})
	if err := extractGatewayTarGz(archive, dest); err != nil {
		t.Fatalf("extractGatewayTarGz() error = %v", err)
	}

	after, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Error("the old binary was rewritten in place, want a new file renamed over it")
	}
	if got, _ := os.ReadFile(dest); string(got) != testNewGatewayBinary {
		t.Errorf("binary content = %q, want %q", got, testNewGatewayBinary)
	}
	if after.Mode().Perm()&testExecutableBits == 0 {
		t.Errorf("binary mode = %v, want it executable", after.Mode())
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != testFilesAfterUpdate {
		t.Errorf("directory holds %d files after the update, want only the binary", len(left))
	}
}

func TestExtractGatewayTarGzKeepsTheOldBinaryWhenTheArchiveHasNone(t *testing.T) {
	dest := filepath.Join(t.TempDir(), testGatewayBinaryName)
	if err := os.WriteFile(dest, []byte(testOldGatewayBinary), gatewayBinaryMode); err != nil {
		t.Fatal(err)
	}

	archive := gatewayReleaseArchive(t, map[string]string{testOtherArchiveFile: testNewGatewayBinary})
	if err := extractGatewayTarGz(archive, dest); err == nil {
		t.Fatal("extractGatewayTarGz() error = nil, want an error for an archive without the binary")
	}

	if got, _ := os.ReadFile(dest); string(got) != testOldGatewayBinary {
		t.Errorf("binary content = %q, want the old binary left alone", got)
	}
}

func TestProbeCachedBinary(t *testing.T) {
	if runtime.GOOS == windowsOperatingSystem {
		t.Skip("a shell script stands in for the binary")
	}
	tests := []struct {
		name   string
		script string
		want   gatewaydomain.CachedBinary
	}{
		{"nothing is cached", "", gatewaydomain.CachedBinary{}},
		{"a binary that says its version", testVersionedBinary, gatewaydomain.CachedBinary{Present: true, Version: testLatestVersion}},
		{"a binary the system kills at start", testSelfKillingBinary, gatewaydomain.CachedBinary{Present: true, KilledAtStart: true}},
		{"a binary that fails on its own", testFailingBinary, gatewaydomain.CachedBinary{Present: true}},
		{"a binary that prints nothing", testSilentBinary, gatewaydomain.CachedBinary{Present: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binary := filepath.Join(t.TempDir(), testGatewayBinaryName)
			if tt.script != "" {
				if err := os.WriteFile(binary, []byte(tt.script), gatewayBinaryMode); err != nil {
					t.Fatal(err)
				}
			}

			if got := probeCachedBinary(t.Context(), binary); got != tt.want {
				t.Errorf("probeCachedBinary() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
