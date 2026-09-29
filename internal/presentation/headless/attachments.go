package headless

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// maxAttachmentBytes caps one decoded attachment. The panel enforces the same
// limit, and this is the trust-boundary check.
const maxAttachmentBytes = 10 * 1024 * 1024

// unsafeFilenameChars matches everything outside the portable filename set.
var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeFilename reduces a panel-supplied filename to a single path segment made
// of portable characters, so it can never escape the tmp dir.
func safeFilename(name string) string {
	name = unsafeFilenameChars.ReplaceAllString(filepath.Base(name), "_")
	if name == "" || name == "." || strings.Contains(name, "..") {
		return "file"
	}
	return name
}

// modelImageMimeTypes are the image formats providers accept as image content
// parts. Anything else is handed to the agent as a file path instead.
var modelImageMimeTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// saveAttachments writes each attachment into the project tmp dir (where
// clipboard images also land). Images come back as ImageAttachments with
// SourcePath set so they flow to the model as image parts. Other files come
// back as text notes naming the saved path so the agent can Read them.
func saveAttachments(attachments []agentdomain.ImageAttachment) ([]agentdomain.ImageAttachment, []string) {
	if len(attachments) == 0 {
		return nil, nil
	}
	tmpDir := config.ProjectTmpDir()
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		logger.Warn("failed to create tmp directory", "path", tmpDir, "error", err)
		return nil, nil
	}
	var images []agentdomain.ImageAttachment
	var notes []string
	stamp := time.Now().Format("20060102-150405")
	for i, a := range attachments {
		if a.Data == "" || a.Filename == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil || len(data) > maxAttachmentBytes {
			logger.Warn("skipping panel attachment", "filename", a.Filename, "error", err, "bytes", len(data))
			continue
		}
		name := safeFilename(a.Filename)
		path := filepath.Join(tmpDir, fmt.Sprintf("attachment-%s-%d-%s", stamp, i, name))
		if err := os.WriteFile(path, data, 0644); err != nil {
			logger.Warn("failed to save panel attachment", "path", path, "error", err)
			continue
		}
		switch {
		case modelImageMimeTypes[a.MimeType]:
			a.DisplayName = a.Filename
			a.SourcePath = path
			images = append(images, a)
		case strings.HasPrefix(a.MimeType, "image/"):
			notes = append(notes, fmt.Sprintf("[%s saved at %s; %s is not a model-readable image format, convert it to PNG first (e.g. sips -s format png on macOS, or magick) and then view the PNG]", name, path, a.MimeType))
		default:
			notes = append(notes, fmt.Sprintf("[%s saved at %s]", name, path))
		}
	}
	utils.PruneFilesByModTime(tmpDir, 20, 24*time.Hour, func(e os.DirEntry) bool {
		return strings.HasPrefix(e.Name(), "attachment-")
	})
	return images, notes
}
