package download

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

// FilenameFromURL extracts a safe filename from a URL. When the URL path has no
// extension, the response Content-Type is used to derive one (e.g. image/png
// -> .png), falling back to .dat for unknown types.
func FilenameFromURL(url, contentType string) string {
	parts := strings.Split(url, "/")
	filename := "download"

	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			if idx := strings.Index(parts[i], "?"); idx != -1 {
				parts[i] = parts[i][:idx]
			}
			if idx := strings.Index(parts[i], "#"); idx != -1 {
				parts[i] = parts[i][:idx]
			}
			if parts[i] != "" {
				filename = parts[i]
				break
			}
		}
	}

	filename = filepath.Base(filename)

	if !strings.Contains(filename, ".") {
		ext := extensionFromContentType(contentType)
		filename = fmt.Sprintf("%s%s", filename, ext)
	}

	return filename
}

// extensionFromContentType derives a file extension from a MIME content type
// using the standard library, returning ".dat" for unknown types.
func extensionFromContentType(contentType string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	exts, err := mime.ExtensionsByType(ct)
	if err != nil || len(exts) == 0 {
		return ".dat"
	}
	return exts[0]
}
