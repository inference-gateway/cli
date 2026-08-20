// Package clipboard provides cgo-free clipboard access. Text goes through
// robotgo's exec-based clipboard subpackage (pbcopy/pbpaste, xclip/xsel,
// Windows API); image reads shell out to the platform's clipboard tool
// (osascript, wl-paste/xclip, PowerShell). Image writes are not supported.
package clipboard

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	rgclip "github.com/go-vgo/robotgo/clipboard"
)

// Format represents a clipboard data format.
type Format int

const (
	// FmtText is UTF-8 text.
	FmtText Format = iota
	// FmtImage is PNG image data. Reads are supported; writes are ignored.
	FmtImage
)

// Init is a no-op; the clipboard backend needs no initialization.
func Init() error { return nil }

// Read returns the clipboard contents for the given format, or nil if the
// clipboard holds no data in that format.
func Read(format Format) []byte {
	if format == FmtImage {
		return readImage()
	}
	s, err := rgclip.ReadAll()
	if err != nil {
		return nil
	}
	return []byte(s)
}

// Write sets the clipboard for the given format. Image writes are ignored.
func Write(format Format, data []byte) {
	if format == FmtText {
		_ = rgclip.WriteAll(string(data))
	}
}

// readImage returns the clipboard's image contents as PNG bytes via the
// platform clipboard tool, or nil when the clipboard holds no image.
func readImage() []byte {
	switch runtime.GOOS {
	case "darwin":
		return readImageViaTempFile(func(path string) *exec.Cmd {
			script := fmt.Sprintf(`set pngData to the clipboard as «class PNGf»
set f to open for access POSIX file %q with write permission
write pngData to f
close access f`, path)
			return exec.Command("osascript", "-e", script)
		})
	case "linux":
		if out, err := exec.Command("wl-paste", "-t", "image/png").Output(); err == nil && len(out) > 0 {
			return out
		}
		if out, err := exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o").Output(); err == nil && len(out) > 0 {
			return out
		}
		return nil
	case "windows":
		return readImageViaTempFile(func(path string) *exec.Cmd {
			script := fmt.Sprintf(`$img = Get-Clipboard -Format Image
if ($img -eq $null) { exit 1 }
$img.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)`, path)
			return exec.Command("powershell", "-NoProfile", "-Command", script)
		})
	default:
		return nil
	}
}

// readImageViaTempFile runs a command that writes the clipboard image to the
// given path and returns the file's contents.
func readImageViaTempFile(cmd func(path string) *exec.Cmd) []byte {
	f, err := os.CreateTemp("", "infer-clipboard-*.png")
	if err != nil {
		return nil
	}
	path := f.Name()
	_ = f.Close()
	defer func() { _ = os.Remove(path) }()

	if err := cmd(path).Run(); err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}
