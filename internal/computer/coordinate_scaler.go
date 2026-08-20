package computer

import (
	"fmt"
	"math"
)

// ScaleAPIToScreen converts coordinates from API space (the model's screenshot)
// to screen space (actual display) with proportional scaling. Frames are
// resized uniformly (ScreenshotToolConfig.FitDims), so the X and Y factors
// are equal in practice; independent factors are kept for identity mapping
// when no resize happened.
//
// Parameters:
//   - apiX, apiY: Coordinates from the model's response (in screenshot space)
//   - apiWidth, apiHeight: Screenshot dimensions from FitDims
//   - screenWidth, screenHeight: Actual logical screen dimensions
//
// Returns:
//   - screenX, screenY: Coordinates in actual screen space for mouse operations
//   - error when the input lies outside the API space; the message stays in API
//     space so a model that never saw the real display cannot misread it as
//     screen bounds
//
// Example:
//
//	API screenshot: 1024x768
//	Actual screen: 2048x1536
//	API coordinate (512, 384) → Screen coordinate (1024, 768)
func ScaleAPIToScreen(apiX, apiY, apiWidth, apiHeight, screenWidth, screenHeight int) (int, int, error) {
	if apiX < 0 || apiY < 0 || apiX > apiWidth || apiY > apiHeight {
		return 0, 0, fmt.Errorf(
			"coordinates (%d,%d) are outside the %dx%d screenshot coordinate space (same space as GetLatestFrame bounding boxes)",
			apiX, apiY, apiWidth, apiHeight)
	}

	xScale := float64(apiWidth) / float64(screenWidth)
	yScale := float64(apiHeight) / float64(screenHeight)

	screenX := int(math.Round(float64(apiX) / xScale))
	screenY := int(math.Round(float64(apiY) / yScale))

	return screenX, screenY, nil
}
