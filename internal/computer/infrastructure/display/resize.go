package display

import (
	"image"
	"image/color"
)

// ResizeImage resizes an image to target dimensions using bilinear interpolation
// This provides better quality than nearest neighbor for LLM visual understanding
func ResizeImage(src image.Image, targetWidth, targetHeight int) image.Image {
	srcBounds := src.Bounds()
	srcWidth := srcBounds.Dx()
	srcHeight := srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	xRatio := float64(srcWidth-1) / float64(targetWidth-1)
	yRatio := float64(srcHeight-1) / float64(targetHeight-1)

	for dstY := range targetHeight {
		for dstX := range targetWidth {
			srcXFloat := float64(dstX) * xRatio
			srcYFloat := float64(dstY) * yRatio

			srcX := int(srcXFloat)
			srcY := int(srcYFloat)

			fracX := srcXFloat - float64(srcX)
			fracY := srcYFloat - float64(srcY)

			srcX1 := srcX
			srcY1 := srcY
			srcX2 := srcX + 1
			srcY2 := srcY + 1

			if srcX2 >= srcWidth {
				srcX2 = srcWidth - 1
			}
			if srcY2 >= srcHeight {
				srcY2 = srcHeight - 1
			}

			c11 := src.At(srcBounds.Min.X+srcX1, srcBounds.Min.Y+srcY1)
			c21 := src.At(srcBounds.Min.X+srcX2, srcBounds.Min.Y+srcY1)
			c12 := src.At(srcBounds.Min.X+srcX1, srcBounds.Min.Y+srcY2)
			c22 := src.At(srcBounds.Min.X+srcX2, srcBounds.Min.Y+srcY2)

			r11, g11, b11, a11 := c11.RGBA()
			r21, g21, b21, a21 := c21.RGBA()
			r12, g12, b12, a12 := c12.RGBA()
			r22, g22, b22, a22 := c22.RGBA()

			w1 := (1 - fracX) * (1 - fracY)
			w2 := fracX * (1 - fracY)
			w3 := (1 - fracX) * fracY
			w4 := fracX * fracY

			r := uint8((float64(r11)*w1 + float64(r21)*w2 + float64(r12)*w3 + float64(r22)*w4) / 257)
			g := uint8((float64(g11)*w1 + float64(g21)*w2 + float64(g12)*w3 + float64(g22)*w4) / 257)
			b := uint8((float64(b11)*w1 + float64(b21)*w2 + float64(b12)*w3 + float64(b22)*w4) / 257)
			a := uint8((float64(a11)*w1 + float64(a21)*w2 + float64(a12)*w3 + float64(a22)*w4) / 257)

			dst.SetRGBA(dstX, dstY, color.RGBA{R: r, G: g, B: b, A: a})
		}
	}

	return dst
}
