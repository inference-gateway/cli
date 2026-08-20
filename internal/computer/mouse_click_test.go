package computer

import (
	"context"
	"errors"
	"image"
	"testing"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	display "github.com/inference-gateway/cli/internal/computer/infrastructure/display"
)

// dimsController stubs DisplayController; only GetScreenDimensions matters here.
type dimsController struct {
	width, height int
	err           error
}

func (c *dimsController) CaptureScreenBytes(context.Context, *display.Region) ([]byte, error) {
	return nil, nil
}
func (c *dimsController) CaptureScreen(context.Context, *display.Region) (image.Image, error) {
	return nil, nil
}
func (c *dimsController) GetScreenDimensions(context.Context) (int, int, error) {
	return c.width, c.height, c.err
}
func (c *dimsController) GetCursorPosition(context.Context) (int, int, error) { return 0, 0, nil }
func (c *dimsController) MoveMouse(context.Context, int, int) error           { return nil }
func (c *dimsController) ClickMouse(context.Context, display.MouseButton, int) error {
	return nil
}
func (c *dimsController) ScrollMouse(context.Context, int, string) error { return nil }
func (c *dimsController) TypeText(context.Context, string, int) error    { return nil }
func (c *dimsController) SendKeyCombo(context.Context, string) error     { return nil }
func (c *dimsController) Close() error                                   { return nil }

func newMouseClickTestTool() *MouseClickTool {
	cfg := config.DefaultConfig()
	cfg.ComputerUse = *config.DefaultComputerUseConfig()
	return &MouseClickTool{config: cfg}
}

func TestMouseClickGetButton(t *testing.T) {
	tool := newMouseClickTestTool()
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing defaults to left", map[string]any{}, "left"},
		{"explicit right", map[string]any{"button": "right"}, "right"},
		{"explicit middle", map[string]any{"button": "middle"}, "middle"},
		{"wrong type defaults to left", map[string]any{"button": 2.0}, "left"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tool.getButton(tt.args); got != tt.want {
				t.Fatalf("getButton(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestMouseClickGetClicks(t *testing.T) {
	tool := newMouseClickTestTool()
	tests := []struct {
		name string
		args map[string]any
		want int
	}{
		{"missing defaults to 1", map[string]any{}, 1},
		{"json number", map[string]any{"clicks": 2.0}, 2},
		{"go int is not a json number, defaults", map[string]any{"clicks": 3}, 1},
		{"wrong type defaults to 1", map[string]any{"clicks": "2"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tool.getClicks(tt.args); got != tt.want {
				t.Fatalf("getClicks(%v) = %d, want %d", tt.args, got, tt.want)
			}
		})
	}
}

func TestMouseClickGetCoordinates(t *testing.T) {
	tool := newMouseClickTestTool()
	tests := []struct {
		name           string
		args           map[string]any
		wantX, wantY   int
		wantShouldMove bool
	}{
		{"both present", map[string]any{"x": 100.0, "y": 200.0}, 100, 200, true},
		{"missing both", map[string]any{}, 0, 0, false},
		{"x only", map[string]any{"x": 100.0}, 0, 0, false},
		{"y only", map[string]any{"y": 200.0}, 0, 0, false},
		{"wrong-typed x", map[string]any{"x": "100", "y": 200.0}, 0, 0, false},
		{"zero origin", map[string]any{"x": 0.0, "y": 0.0}, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, shouldMove := tool.getCoordinates(tt.args)
			if x != tt.wantX || y != tt.wantY || shouldMove != tt.wantShouldMove {
				t.Fatalf("getCoordinates(%v) = (%d,%d,%v), want (%d,%d,%v)",
					tt.args, x, y, shouldMove, tt.wantX, tt.wantY, tt.wantShouldMove)
			}
		})
	}
}

func TestMouseClickScaleCoordinates(t *testing.T) {
	tool := newMouseClickTestTool() // default target 1024x768

	directCtx := context.WithValue(context.Background(), agentdomain.DirectExecutionKey, true)

	tests := []struct {
		name         string
		ctx          context.Context
		controller   display.DisplayController
		x, y         int
		wantX, wantY int
		wantErr      bool
	}{
		// direct execution bypasses scaling entirely; nil controller proves it is untouched
		{"direct execution passthrough", directCtx, nil, 400, 300, 400, 300, false},
		// 1512x982 screen → FitDims gives a 1024x665 API space
		{"retina scale up", context.Background(), &dimsController{width: 1512, height: 982}, 512, 332, 756, 490, false},
		{"api-space max corner", context.Background(), &dimsController{width: 1512, height: 982}, 1024, 665, 1512, 982, false},
		{"identity when screen fits target", context.Background(), &dimsController{width: 800, height: 600}, 400, 300, 400, 300, false},
		{"dims error degrades to passthrough", context.Background(), &dimsController{err: errors.New("no display")}, 500, 500, 500, 500, false},
		{"out of api space", context.Background(), &dimsController{width: 1512, height: 982}, 1500, 100, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y, err := tool.scaleCoordinates(tt.ctx, tt.controller, tt.x, tt.y)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (x != tt.wantX || y != tt.wantY) {
				t.Fatalf("scaleCoordinates(%d,%d) = (%d,%d), want (%d,%d)", tt.x, tt.y, x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestMouseClickValidate(t *testing.T) {
	tool := newMouseClickTestTool()
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"empty args", map[string]any{}, ""},
		{"valid full", map[string]any{"button": "right", "clicks": 2.0, "x": 10.0, "y": 20.0}, ""},
		{"invalid button", map[string]any{"button": "back"}, "button must be"},
		{"clicks too low", map[string]any{"clicks": 0.0}, "clicks must be"},
		{"clicks too high", map[string]any{"clicks": 4.0}, "clicks must be"},
		{"x without y", map[string]any{"x": 10.0}, "both x and y"},
		{"y without x", map[string]any{"y": 10.0}, "both x and y"},
		{"negative x", map[string]any{"x": -1.0, "y": 10.0}, "x coordinate must be"},
		{"negative y", map[string]any{"x": 10.0, "y": -1.0}, "y coordinate must be"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate(%v) = %v, want nil", tt.args, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate(%v) = nil, want error containing %q", tt.args, tt.wantErr)
			}
		})
	}
}
