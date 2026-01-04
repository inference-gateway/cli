//go:build darwin

package macos

import (
	"fmt"
	"os/exec"
	"syscall"
	"time"

	logger "github.com/inference-gateway/cli/internal/logger"
)

// OverlayWindow represents a macOS overlay indicator using osascript
type OverlayWindow struct {
	cmd     *exec.Cmd
	visible bool
}

// NewOverlayWindow creates a new macOS overlay window using persistent alert
func NewOverlayWindow() (*OverlayWindow, error) {
	logger.Info("Creating macOS overlay window")
	return &OverlayWindow{
		visible: false,
	}, nil
}

// Show displays a screen border overlay using Swift
func (w *OverlayWindow) Show() error {
	logger.Info("Attempting to show macOS screen border overlay")

	swiftScript := `
import Cocoa
import Foundation

class BorderWindow: NSWindow {
    init(frame: NSRect, color: NSColor) {
        super.init(contentRect: frame, styleMask: .borderless, backing: .buffered, defer: false)
        self.backgroundColor = color
        self.isOpaque = false
        self.level = .floating
        self.ignoresMouseEvents = true
        self.collectionBehavior = [.canJoinAllSpaces, .stationary]
        self.orderFront(nil)
    }
}

signal(SIGTERM, SIG_IGN)
let sigTermSource = DispatchSource.makeSignalSource(signal: SIGTERM, queue: .main)
sigTermSource.setEventHandler {
    exit(0)
}
sigTermSource.resume()

let screen = NSScreen.main!
let frame = screen.visibleFrame
let borderWidth: CGFloat = 3
let borderColor = NSColor(red: 0.3, green: 0.6, blue: 1.0, alpha: 0.95)

_ = BorderWindow(frame: NSRect(x: frame.minX, y: frame.maxY - borderWidth, width: frame.width, height: borderWidth), color: borderColor)
_ = BorderWindow(frame: NSRect(x: frame.minX, y: frame.minY, width: frame.width, height: borderWidth), color: borderColor)
_ = BorderWindow(frame: NSRect(x: frame.minX, y: frame.minY, width: borderWidth, height: frame.height), color: borderColor)
_ = BorderWindow(frame: NSRect(x: frame.maxX - borderWidth, y: frame.minY, width: borderWidth, height: frame.height), color: borderColor)

RunLoop.main.run()
`

	logger.Debug("Compiling and running Swift screen border overlay")

	cmd := exec.Command("swift", "-")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		logger.Error("Failed to create stdin pipe", "error", err)
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		logger.Error("Failed to start Swift process", "error", err)
		return fmt.Errorf("failed to start Swift process: %w", err)
	}

	go func() {
		defer func() { _ = stdin.Close() }()
		if _, err := stdin.Write([]byte(swiftScript)); err != nil {
			logger.Error("Failed to write Swift script to stdin", "error", err)
		}
	}()

	w.cmd = cmd
	w.visible = true
	logger.Info("Screen border overlay shown successfully")
	return nil
}

// Hide hides the screen border overlay by terminating the process gracefully
func (w *OverlayWindow) Hide() error {
	logger.Info("Hiding macOS screen border overlay")

	if w.cmd == nil {
		w.visible = false
		return nil
	}

	if w.cmd.Process == nil {
		w.cmd = nil
		w.visible = false
		return nil
	}

	cmd := w.cmd
	w.cmd = nil
	w.visible = false

	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		logger.Debug("Failed to send SIGTERM, using SIGKILL", "error", err)
		if err := cmd.Process.Kill(); err != nil {
			logger.Warn("Failed to kill overlay process", "error", err)
			return fmt.Errorf("failed to kill overlay process: %w", err)
		}
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			logger.Debug("Overlay process exited with error", "error", err)
		} else {
			logger.Debug("Overlay process exited cleanly")
		}
	case <-time.After(5 * time.Second):
		logger.Warn("Overlay process did not exit within timeout, force killing")
		if err := cmd.Process.Kill(); err != nil {
			logger.Error("Failed to force kill overlay process", "error", err)
			return fmt.Errorf("failed to force kill overlay process: %w", err)
		}
		<-done
	}

	return nil
}

// IsVisible returns whether the overlay is currently visible
func (w *OverlayWindow) IsVisible() bool {
	return w.visible
}

// Destroy cleans up the screen border overlay by killing the process
func (w *OverlayWindow) Destroy() error {
	logger.Info("Destroying macOS screen border overlay")
	return w.Hide()
}
