package services

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// DirectoryFrameSource serves the newest image file under a configured
// directory - the integration surface for camera daemons and anything else
// that drops frames on disk. An optional retention sweep (max_files/max_age)
// runs after each read; omit retention when the producer manages cleanup.
type DirectoryFrameSource struct {
	name     string
	path     string
	maxFiles int
	maxAge   time.Duration
	images   agentdomain.ImageService
}

// NewDirectoryFrameSource creates a frame source over cfg.Path. The retention
// max_age was validated at config load, so a parse failure here means zero (no
// age limit).
func NewDirectoryFrameSource(name string, cfg config.VisionSourceConfig, images agentdomain.ImageService) *DirectoryFrameSource {
	maxAge, _ := time.ParseDuration(cfg.Retention.MaxAge)
	return &DirectoryFrameSource{
		name:     name,
		path:     expandHomePath(cfg.Path),
		maxFiles: cfg.Retention.MaxFiles,
		maxAge:   maxAge,
		images:   images,
	}
}

// GetLatestFrame returns the newest image file in the directory by mtime.
func (d *DirectoryFrameSource) GetLatestFrame() (*agentdomain.Frame, error) {
	entries, err := os.ReadDir(d.path)
	if err != nil {
		return nil, fmt.Errorf("reading frame source %q directory: %w", d.name, err)
	}

	var newest os.DirEntry
	var newestTime time.Time
	for _, e := range entries {
		if e.IsDir() || !d.images.IsImageFile(filepath.Join(d.path, e.Name())) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().After(newestTime) {
			newest = e
			newestTime = info.ModTime()
		}
	}
	if newest == nil {
		return nil, fmt.Errorf("no image files in frame source %q (%s)", d.name, d.path)
	}

	fullPath := filepath.Join(d.path, newest.Name())
	attachment, err := d.images.ReadImageFromFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("reading frame %s: %w", fullPath, err)
	}

	width, height := decodeImageDims(attachment.Data)
	frame := &agentdomain.Frame{
		ID:        newest.Name(),
		Timestamp: newestTime,
		Data:      attachment.Data,
		Path:      fullPath,
		Width:     width,
		Height:    height,
		Format:    strings.TrimPrefix(attachment.MimeType, "image/"),
		Method:    "directory",
	}

	defer pruneFilesByModTime(d.path, d.maxFiles, d.maxAge, func(e os.DirEntry) bool {
		return d.images.IsImageFile(filepath.Join(d.path, e.Name()))
	})

	return frame, nil
}

// decodeImageDims sniffs pixel dimensions from base64 image data (0,0 when unknown).
func decodeImageDims(b64 string) (int, int) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return 0, 0
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return 0, 0
	}
	return cfg.Width, cfg.Height
}

// expandHomePath expands a leading ~/ to the user's home directory.
func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
