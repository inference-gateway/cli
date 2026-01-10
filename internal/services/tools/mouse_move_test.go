package tools

import (
	"context"
	"testing"

	config "github.com/inference-gateway/cli/config"
	display "github.com/inference-gateway/cli/internal/display"
	domain "github.com/inference-gateway/cli/internal/domain"
	displayMocks "github.com/inference-gateway/cli/tests/mocks/display"
	domainMocks "github.com/inference-gateway/cli/tests/mocks/domain"
)

func TestMouseMoveTool_CoordinateScaling(t *testing.T) {
	tests := []struct {
		name          string
		apiWidth      int
		apiHeight     int
		logicalWidth  int
		logicalHeight int
		inputX        float64
		inputY        float64
		directExec    bool
		expectedX     int
		expectedY     int
		description   string
	}{
		{
			name:          "Direct execution - no scaling",
			apiWidth:      1024,
			apiHeight:     768,
			logicalWidth:  1728,
			logicalHeight: 1117,
			inputX:        500,
			inputY:        400,
			directExec:    true,
			expectedX:     500,
			expectedY:     400,
			description:   "Direct execution should use coordinates as-is without scaling",
		},
		{
			name:          "LLM execution - scale from XGA to 2x Retina",
			apiWidth:      1024,
			apiHeight:     768,
			logicalWidth:  2048,
			logicalHeight: 1536,
			inputX:        512,
			inputY:        384,
			directExec:    false,
			expectedX:     1024,
			expectedY:     768,
			description:   "LLM execution should scale from XGA (1024x768) to 2x Retina (2048x1536)",
		},
		{
			name:          "LLM execution - top-left corner",
			apiWidth:      1024,
			apiHeight:     768,
			logicalWidth:  1728,
			logicalHeight: 1117,
			inputX:        0,
			inputY:        0,
			directExec:    false,
			expectedX:     0,
			expectedY:     0,
			description:   "Top-left corner should map to (0,0) in both spaces",
		},
		{
			name:          "LLM execution - mismatched aspect ratios (proportional scaling)",
			apiWidth:      1024,
			apiHeight:     768,
			logicalWidth:  1728,
			logicalHeight: 1117,
			inputX:        1024,
			inputY:        768,
			directExec:    false,
			expectedX:     1728,
			expectedY:     1117,
			description:   "Bottom-right corner of API space maps to bottom-right corner of screen space",
		},
		{
			name:          "LLM execution - 3x scaling",
			apiWidth:      1024,
			apiHeight:     768,
			logicalWidth:  3072,
			logicalHeight: 2304,
			inputX:        256,
			inputY:        192,
			directExec:    false,
			expectedX:     768,
			expectedY:     576,
			description:   "3x scaling should work proportionally with matching aspect ratios",
		},
		{
			name:          "No scaling when dimensions match",
			apiWidth:      1728,
			apiHeight:     1117,
			logicalWidth:  1728,
			logicalHeight: 1117,
			inputX:        500,
			inputY:        400,
			directExec:    false,
			expectedX:     500,
			expectedY:     400,
			description:   "When API and logical dimensions match, no scaling should occur",
		},
		{
			name:          "LLM execution - config has zero dimensions",
			apiWidth:      0,
			apiHeight:     0,
			logicalWidth:  1728,
			logicalHeight: 1117,
			inputX:        500,
			inputY:        400,
			directExec:    false,
			expectedX:     500,
			expectedY:     400,
			description:   "When config dimensions are 0, no scaling should occur",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := coordinateScalingTestCase{
				apiWidth:      tt.apiWidth,
				apiHeight:     tt.apiHeight,
				logicalWidth:  tt.logicalWidth,
				logicalHeight: tt.logicalHeight,
				inputX:        tt.inputX,
				inputY:        tt.inputY,
				directExec:    tt.directExec,
				expectedX:     tt.expectedX,
				expectedY:     tt.expectedY,
				description:   tt.description,
			}
			runCoordinateScalingTest(t, tc)
		})
	}
}

type coordinateScalingTestCase struct {
	apiWidth      int
	apiHeight     int
	logicalWidth  int
	logicalHeight int
	inputX        float64
	inputY        float64
	directExec    bool
	expectedX     int
	expectedY     int
	description   string
}

func runCoordinateScalingTest(t *testing.T, tt coordinateScalingTestCase) {
	t.Helper()

	mockCtrl := &displayMocks.FakeDisplayController{}
	mockCtrl.GetScreenDimensionsReturns(tt.logicalWidth, tt.logicalHeight, nil)
	mockCtrl.GetCursorPositionReturns(100, 100, nil)
	mockCtrl.MoveMouseReturns(nil)
	mockCtrl.CloseReturns(nil)

	mockProv := &displayMocks.FakeProvider{}
	mockProv.GetControllerReturns(mockCtrl, nil)
	mockProv.GetDisplayInfoReturns(display.DisplayInfo{Name: "mock"})

	cfg := &config.Config{
		ComputerUse: config.ComputerUseConfig{
			Screenshot: config.ScreenshotToolConfig{
				TargetWidth:  tt.apiWidth,
				TargetHeight: tt.apiHeight,
			},
		},
	}

	rateLimiter := &domainMocks.FakeRateLimiter{}
	rateLimiter.CheckAndRecordReturns(nil)

	stateManager := &domainMocks.FakeStateManager{}

	tool := NewMouseMoveTool(cfg, rateLimiter, mockProv, stateManager)

	ctx := context.Background()
	if tt.directExec {
		ctx = context.WithValue(ctx, domain.DirectExecutionKey, true)
	}

	args := map[string]any{
		"x": tt.inputX,
		"y": tt.inputY,
	}

	result, err := tool.Execute(ctx, args)

	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Success {
		t.Fatalf("Execute was not successful: %s", result.Error)
	}

	if mockCtrl.MoveMouseCallCount() != 1 {
		t.Fatalf("Expected MoveMouse to be called once, got %d calls", mockCtrl.MoveMouseCallCount())
	}

	_, actualX, actualY := mockCtrl.MoveMouseArgsForCall(0)

	if actualX != tt.expectedX {
		t.Errorf("%s\nExpected X coordinate: %d, got: %d", tt.description, tt.expectedX, actualX)
	}

	if actualY != tt.expectedY {
		t.Errorf("%s\nExpected Y coordinate: %d, got: %d", tt.description, tt.expectedY, actualY)
	}
}
