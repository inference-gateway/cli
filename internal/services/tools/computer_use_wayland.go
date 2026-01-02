package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// WaylandClient provides Wayland screen control operations using command-line tools
type WaylandClient struct {
	display string
}

// NewWaylandClient creates a new Wayland client
func NewWaylandClient(display string) (*WaylandClient, error) {
	if err := checkWaylandTools(); err != nil {
		return nil, err
	}

	return &WaylandClient{
		display: display,
	}, nil
}

// checkWaylandTools checks if required Wayland tools are available
func checkWaylandTools() error {
	tools := []string{"grim"}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("required tool '%s' not found in PATH (install with: sudo apt install %s)", tool, tool)
		}
	}

	return nil
}

// Close closes the Wayland client (no-op for command-line tools)
func (c *WaylandClient) Close() {
	// Nothing to close for command-line tools
}

// CaptureScreenBytes captures a screenshot and returns it as PNG bytes
func (c *WaylandClient) CaptureScreenBytes(x, y, width, height int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if width == 0 || height == 0 {
		cmd = exec.CommandContext(ctx, "grim", "-")
	} else {
		geometry := fmt.Sprintf("%d,%d %dx%d", x, y, width, height)
		cmd = exec.CommandContext(ctx, "grim", "-g", geometry, "-")
	}

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("grim failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("failed to capture screenshot: %w", err)
	}

	return output, nil
}

// MoveMouse moves the cursor to the specified absolute coordinates
func (c *WaylandClient) MoveMouse(x, y int) error {
	if _, err := exec.LookPath("ydotool"); err != nil {
		return fmt.Errorf("ydotool not found (install with: sudo apt install ydotool)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ydotool", "mousemove", "--absolute", "--",
		strconv.Itoa(x), strconv.Itoa(y))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ydotool mousemove failed: %s", string(output))
	}

	return nil
}

// ClickMouse performs a mouse click at the current cursor position
func (c *WaylandClient) ClickMouse(button string, clicks int) error {
	if _, err := exec.LookPath("ydotool"); err != nil {
		return fmt.Errorf("ydotool not found (install with: sudo apt install ydotool)")
	}

	var buttonCode string
	switch button {
	case "left":
		buttonCode = "0xC0"
	case "middle":
		buttonCode = "0xC1"
	case "right":
		buttonCode = "0xC2"
	default:
		return fmt.Errorf("invalid button: %s (must be left, middle, or right)", button)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < clicks; i++ {
		cmd := exec.CommandContext(ctx, "ydotool", "click", buttonCode)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("ydotool click failed: %s", string(output))
		}

		if i < clicks-1 {
			time.Sleep(100 * time.Millisecond)
		}
	}

	return nil
}

// TypeText types the given text
func (c *WaylandClient) TypeText(text string) error {
	if _, err := exec.LookPath("wtype"); err == nil {
		return c.typeTextWithWtype(text)
	}

	if _, err := exec.LookPath("ydotool"); err == nil {
		return c.typeTextWithYdotool(text)
	}

	return fmt.Errorf("no text input tool available (install wtype or ydotool)")
}

// typeTextWithWtype types text using the wtype command
func (c *WaylandClient) typeTextWithWtype(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wtype", text)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wtype failed: %s", string(output))
	}

	return nil
}

// typeTextWithYdotool types text using the ydotool command
func (c *WaylandClient) typeTextWithYdotool(text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ydotool", "type", text)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ydotool type failed: %s", string(output))
	}

	return nil
}

// SendKeyCombo sends a key combination (e.g., "ctrl+c")
func (c *WaylandClient) SendKeyCombo(combo string) error {
	if _, err := exec.LookPath("wtype"); err == nil {
		return c.sendKeyComboWithWtype(combo)
	}

	if _, err := exec.LookPath("ydotool"); err == nil {
		return c.sendKeyComboWithYdotool(combo)
	}

	return fmt.Errorf("no key combo tool available (install wtype or ydotool)")
}

// sendKeyComboWithWtype sends a key combination using wtype
func (c *WaylandClient) sendKeyComboWithWtype(combo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	parts := strings.Split(combo, "+")
	if len(parts) < 2 {
		return fmt.Errorf("invalid key combo format: %s (expected format: modifier+key)", combo)
	}

	modifiers := parts[:len(parts)-1]
	key := parts[len(parts)-1]

	args := []string{}
	for _, mod := range modifiers {
		args = append(args, "-M", mod)
	}
	args = append(args, "-P", key)

	cmd := exec.CommandContext(ctx, "wtype", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wtype key combo failed: %s", string(output))
	}

	return nil
}

// sendKeyComboWithYdotool sends a key combination using ydotool
func (c *WaylandClient) sendKeyComboWithYdotool(combo string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// ydotool key combo format: key1:key2
	// Convert "ctrl+c" to "29:46" (keycodes)
	// This is a simplified version - proper implementation would need keycode mapping

	cmd := exec.CommandContext(ctx, "ydotool", "key", combo)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ydotool key combo failed: %s", string(output))
	}

	return nil
}

// GetScreenDimensions returns the screen width and height
func (c *WaylandClient) GetScreenDimensions() (int, int, error) {
	// Wayland doesn't have a simple command to get screen dimensions
	// We can use wlr-randr if available, or return default values

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "wlr-randr")
	output, err := cmd.Output()
	if err != nil {
		return 1920, 1080, nil
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if !strings.Contains(line, "current") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		dims := strings.Split(parts[0], "x")
		if len(dims) != 2 {
			continue
		}

		width, _ := strconv.Atoi(dims[0])
		height, _ := strconv.Atoi(dims[1])
		if width > 0 && height > 0 {
			return width, height, nil
		}
	}

	return 1920, 1080, nil
}
