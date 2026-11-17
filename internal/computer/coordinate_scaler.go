package computer

import (
	"fmt"
	"math"
)

// ScaleAPIToScreen maps a point from the model's screenshot space to the logical
// screen, for example (512, 384) on 1024x768 to (1024, 768) on 2048x1536.
// Frames are resized uniformly, and separate factors only serve the identity
// mapping. A point outside the screenshot is an error worded in screenshot
// space, so a model that never saw the display cannot misread it.
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
