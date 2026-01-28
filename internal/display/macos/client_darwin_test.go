//go:build darwin

package macos

import (
	"testing"
)

func TestScalePhysicalToLogical(t *testing.T) {
	tests := []struct {
		name        string
		scaleFactor float64
		physicalX   int
		physicalY   int
		expectedX   int
		expectedY   int
	}{
		{
			name:        "1x display (no scaling)",
			scaleFactor: 1.0,
			physicalX:   800,
			physicalY:   600,
			expectedX:   800,
			expectedY:   600,
		},
		{
			name:        "2x Retina display - full screen",
			scaleFactor: 2.0,
			physicalX:   2880,
			physicalY:   1800,
			expectedX:   1440,
			expectedY:   900,
		},
		{
			name:        "2x Retina display - center point",
			scaleFactor: 2.0,
			physicalX:   1440,
			physicalY:   900,
			expectedX:   720,
			expectedY:   450,
		},
		{
			name:        "2x Retina display - origin",
			scaleFactor: 2.0,
			physicalX:   0,
			physicalY:   0,
			expectedX:   0,
			expectedY:   0,
		},
		{
			name:        "2x Retina display - arbitrary point",
			scaleFactor: 2.0,
			physicalX:   1800,
			physicalY:   800,
			expectedX:   900,
			expectedY:   400,
		},
		{
			name:        "3x Retina display",
			scaleFactor: 3.0,
			physicalX:   4320,
			physicalY:   2700,
			expectedX:   1440,
			expectedY:   900,
		},
		{
			name:        "3x Retina display - center",
			scaleFactor: 3.0,
			physicalX:   2160,
			physicalY:   1350,
			expectedX:   720,
			expectedY:   450,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &MacOSClient{
				screenWidth:  int(float64(1440) * tt.scaleFactor),
				screenHeight: int(float64(900) * tt.scaleFactor),
				scaleFactor:  tt.scaleFactor,
			}

			gotX, gotY := client.ScalePhysicalToLogical(tt.physicalX, tt.physicalY)

			if gotX != tt.expectedX || gotY != tt.expectedY {
				t.Errorf("ScalePhysicalToLogical(%d, %d) = (%d, %d), want (%d, %d)",
					tt.physicalX, tt.physicalY, gotX, gotY, tt.expectedX, tt.expectedY)
			}
		})
	}
}

func TestGetScaleFactor(t *testing.T) {
	tests := []struct {
		name        string
		scaleFactor float64
	}{
		{"1x display", 1.0},
		{"2x Retina", 2.0},
		{"3x Retina", 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &MacOSClient{scaleFactor: tt.scaleFactor}
			if got := client.GetScaleFactor(); got != tt.scaleFactor {
				t.Errorf("GetScaleFactor() = %v, want %v", got, tt.scaleFactor)
			}
		})
	}
}

func TestGetScreenDimensions(t *testing.T) {
	tests := []struct {
		name           string
		scaleFactor    float64
		logicalWidth   int
		logicalHeight  int
		expectedWidth  int
		expectedHeight int
	}{
		{
			name:           "1x display",
			scaleFactor:    1.0,
			logicalWidth:   1920,
			logicalHeight:  1080,
			expectedWidth:  1920,
			expectedHeight: 1080,
		},
		{
			name:           "2x Retina display",
			scaleFactor:    2.0,
			logicalWidth:   1440,
			logicalHeight:  900,
			expectedWidth:  1440,
			expectedHeight: 900,
		},
		{
			name:           "3x Retina display",
			scaleFactor:    3.0,
			logicalWidth:   1440,
			logicalHeight:  900,
			expectedWidth:  1440,
			expectedHeight: 900,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			physicalWidth := int(float64(tt.logicalWidth) * tt.scaleFactor)
			physicalHeight := int(float64(tt.logicalHeight) * tt.scaleFactor)

			client := &MacOSClient{
				screenWidth:  physicalWidth,
				screenHeight: physicalHeight,
				scaleFactor:  tt.scaleFactor,
			}

			gotWidth, gotHeight := client.GetScreenDimensions()

			if gotWidth != tt.expectedWidth || gotHeight != tt.expectedHeight {
				t.Errorf("GetScreenDimensions() = (%d, %d), want (%d, %d)",
					gotWidth, gotHeight, tt.expectedWidth, tt.expectedHeight)
			}
		})
	}
}

func TestScalePhysicalToLogical_RoundingBehavior(t *testing.T) {
	// Test that integer division behaves correctly for odd numbers
	client := &MacOSClient{scaleFactor: 2.0}

	tests := []struct {
		name      string
		physicalX int
		physicalY int
		expectedX int
		expectedY int
	}{
		{
			name:      "Even coordinates",
			physicalX: 100,
			physicalY: 200,
			expectedX: 50,
			expectedY: 100,
		},
		{
			name:      "Odd coordinates (rounds down)",
			physicalX: 101,
			physicalY: 201,
			expectedX: 50,  // 101/2 = 50.5 → 50 (int conversion truncates)
			expectedY: 100, // 201/2 = 100.5 → 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotX, gotY := client.ScalePhysicalToLogical(tt.physicalX, tt.physicalY)

			if gotX != tt.expectedX || gotY != tt.expectedY {
				t.Errorf("ScalePhysicalToLogical(%d, %d) = (%d, %d), want (%d, %d)",
					tt.physicalX, tt.physicalY, gotX, gotY, tt.expectedX, tt.expectedY)
			}
		})
	}
}
