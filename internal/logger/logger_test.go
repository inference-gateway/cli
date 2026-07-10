package logger

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

func fileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestArchiveLogFile(t *testing.T) {
	t.Run("non-existent file is a no-op", func(t *testing.T) {
		if err := archiveLogFile("/tmp/nonexistent-test-file.log", 1024); err != nil {
			t.Fatalf("expected no error for non-existent file, got: %v", err)
		}
	})

	t.Run("file below threshold is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		writeTestFile(t, path, "small log content")

		if err := archiveLogFile(path, 1024); err != nil {
			t.Fatalf("expected no error for small file, got: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "small log content" {
			t.Fatalf("expected file content unchanged, got: %s", string(data))
		}
		if n := fileCount(t, dir); n != 1 {
			t.Fatalf("expected 1 file (no archive), got %d", n)
		}
	})

	t.Run("file above threshold is archived and truncated", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		size := 10 * 1024 * 1024
		writeTestFile(t, path, strings.Repeat("A", size))

		if err := archiveLogFile(path, 1); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if s := fileSize(t, path); s != 0 {
			t.Fatalf("expected truncated file (size 0), got %d", s)
		}
		if n := fileCount(t, dir); n != 2 {
			t.Fatalf("expected 2 files (log + archive), got %d", n)
		}

		matches, err := filepath.Glob(filepath.Join(dir, "*.gz"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("expected exactly one archive file, got %v (err: %v)", matches, err)
		}

		f, err := os.Open(matches[0])
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = f.Close() }()

		gr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatalf("expected valid gzip archive, got: %v", err)
		}
		defer func() { _ = gr.Close() }()

		decompressed, err := io.ReadAll(gr)
		if err != nil {
			t.Fatalf("failed to decompress archive: %v", err)
		}
		if len(decompressed) != size {
			t.Fatalf("expected decompressed size %d, got %d", size, len(decompressed))
		}
	})

	t.Run("zero threshold disables archiving", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		size := 10 * 1024 * 1024
		writeTestFile(t, path, strings.Repeat("B", size))

		if err := archiveLogFile(path, 0); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}

		if s := fileSize(t, path); s != int64(size) {
			t.Fatalf("expected file untouched (size %d), got %d", size, s)
		}
		if n := fileCount(t, dir); n != 1 {
			t.Fatalf("expected 1 file (no archive), got %d", n)
		}
	})
}
