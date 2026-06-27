package tools

import "math"

// ScaleAPIToScreen converts coordinates from API space (Claude's screenshot)
// to screen space (actual display) using Anthropic's proportional scaling approach.
//
// This implementation follows the official Anthropic computer-use-demo strategy:
// - Simple proportional scaling with separate X/Y factors
// - No letterboxing - screenshots are force-resized to exact dimensions
// - Handles aspect ratio mismatches automatically through independent scaling
//
// Parameters:
//   - apiX, apiY: Coordinates from Claude's response (in screenshot space)
//   - apiWidth, apiHeight: Target screenshot dimensions (e.g., 1024x768)
//   - screenWidth, screenHeight: Actual logical screen dimensions
//
// Returns:
//   - screenX, screenY: Coordinates in actual screen space for mouse operations
//
// Example:
//
//	API screenshot: 1024x768
//	Actual screen: 2048x1536
//	API coordinate (512, 384) → Screen coordinate (1024, 768)
func ScaleAPIToScreen(apiX, apiY, apiWidth, apiHeight, screenWidth, screenHeight int) (int, int) {
	xScale := float64(apiWidth) / float64(screenWidth)
	yScale := float64(apiHeight) / float64(screenHeight)

	screenX := int(math.Round(float64(apiX) / xScale))
	screenY := int(math.Round(float64(apiY) / yScale))

	return screenX, screenY
}
