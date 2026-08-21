package infrastructure

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
)

// pngPixel is a minimal 1x1 PNG.
var pngPixel = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89, 0x00, 0x00, 0x00,
	0x0d, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x62, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func newDirSource(t *testing.T, dir string, retention config.VisionRetentionConfig) *DirectoryFrameSource {
	t.Helper()
	imageService := NewImageService(config.DefaultConfig(), nil)
	return NewDirectoryFrameSource("cam", config.VisionSourceConfig{
		Type: "directory", Path: dir, Retention: retention,
	}, imageService)
}

func writeFrame(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, pngPixel, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestDirectoryFrameSourceNewestByMtime(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeFrame(t, dir, "old.png", now.Add(-2*time.Hour))
	writeFrame(t, dir, "new.png", now)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	frame, err := newDirSource(t, dir, config.VisionRetentionConfig{}).GetLatestFrame()
	if err != nil {
		t.Fatalf("GetLatestFrame: %v", err)
	}
	if frame.ID != "new.png" {
		t.Errorf("ID = %q, want new.png", frame.ID)
	}
	if frame.Method != "directory" || frame.Format != "png" {
		t.Errorf("Method/Format = %s/%s", frame.Method, frame.Format)
	}
	if frame.Width != 1 || frame.Height != 1 {
		t.Errorf("dims = %dx%d, want 1x1", frame.Width, frame.Height)
	}
	if frame.Path != filepath.Join(dir, "new.png") {
		t.Errorf("Path = %q", frame.Path)
	}
	if frame.Data == "" {
		t.Error("Data should carry base64 image bytes")
	}
}

func TestDirectoryFrameSourceEmpty(t *testing.T) {
	_, err := newDirSource(t, t.TempDir(), config.VisionRetentionConfig{}).GetLatestFrame()
	if err == nil || !strings.Contains(err.Error(), "no image files") {
		t.Fatalf("expected no-image-files error, got %v", err)
	}
}

func TestDirectoryFrameSourceRetentionMaxFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, name := range []string{"a.png", "b.png", "c.png"} {
		writeFrame(t, dir, name, now.Add(time.Duration(i-3)*time.Minute))
	}

	src := newDirSource(t, dir, config.VisionRetentionConfig{MaxFiles: 2})
	if _, err := src.GetLatestFrame(); err != nil {
		t.Fatalf("GetLatestFrame: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("expected 2 files after retention sweep, got %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(dir, "a.png")); !os.IsNotExist(err) {
		t.Error("oldest file a.png should have been pruned")
	}
}

func TestDirectoryFrameSourceRetentionMaxAge(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeFrame(t, dir, "ancient.png", now.Add(-48*time.Hour))
	writeFrame(t, dir, "fresh.png", now)

	src := newDirSource(t, dir, config.VisionRetentionConfig{MaxAge: "24h"})
	if _, err := src.GetLatestFrame(); err != nil {
		t.Fatalf("GetLatestFrame: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "ancient.png")); !os.IsNotExist(err) {
		t.Error("ancient.png should have been pruned by max_age")
	}
	if _, err := os.Stat(filepath.Join(dir, "fresh.png")); err != nil {
		t.Error("fresh.png should survive")
	}
}
