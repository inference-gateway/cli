package services

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadAndExtractGatewayBinary(t *testing.T) {
	binary := []byte("#!/bin/sh\necho gateway\n")

	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzWriter)
	files := map[string][]byte{
		"README.md":         []byte("readme"),
		"inference-gateway": binary,
	}
	for name, content := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "inference-gateway")
	if err := downloadAndExtractGatewayBinary(context.Background(), server.URL, destPath); err != nil {
		t.Fatalf("downloadAndExtractGatewayBinary failed: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("extracted binary content mismatch: got %q", got)
	}

	info, err := os.Stat(destPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("extracted binary is not executable: %v", info.Mode())
	}
}

func TestDownloadAndExtractGatewayBinaryMissingEntry(t *testing.T) {
	var buf bytes.Buffer
	gzWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "README.md", Mode: 0644, Size: 6}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write([]byte("readme")); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzWriter.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "inference-gateway")
	if err := downloadAndExtractGatewayBinary(context.Background(), server.URL, destPath); err == nil {
		t.Fatal("expected error for archive without the gateway binary")
	}
}

func TestGatewayAssetPlatform(t *testing.T) {
	assetOS, assetArch, err := gatewayAssetPlatform()
	if err != nil {
		t.Fatalf("gatewayAssetPlatform failed on supported platform: %v", err)
	}
	if assetOS != "Darwin" && assetOS != "Linux" {
		t.Fatalf("unexpected asset OS %q", assetOS)
	}
	if assetArch == "" {
		t.Fatal("empty asset arch")
	}
}
