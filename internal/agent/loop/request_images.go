package loop

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"slices"
	"strings"

	draw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	sdk "github.com/inference-gateway/sdk"
)

const requestImageBudget = 2 << 20

// requestImages bounds inline image data in model requests, keeping the newest
// images first. Stored history and the original files remain unchanged.
func requestImages(messages []sdk.Message) []sdk.Message {
	out := slices.Clone(messages)
	remaining := requestImageBudget
	for i := len(out) - 1; i >= 0; i-- {
		parts, err := out[i].Content.AsMessageContent1()
		if err != nil {
			continue
		}
		for j := len(parts) - 1; j >= 0; j-- {
			part, err := parts[j].AsImageContentPart()
			if err != nil || !strings.HasPrefix(part.ImageURL.URL, "data:") {
				continue
			}
			url := compactRequestImage(part.ImageURL.URL)
			if len(url) > remaining {
				parts[j], _ = sdk.NewTextContentPart("[Image omitted from this request to fit the image byte budget. The original remains in conversation history. Reopen the source file or take a focused screenshot if needed.]")
				continue
			}
			remaining -= len(url)
			parts[j], _ = sdk.NewImageContentPart(url, part.ImageURL.Detail)
		}
		out[i].Content = sdk.NewMessageContent(parts)
	}
	return out
}

// compactRequestImage reduces large inline images for model transport only.
func compactRequestImage(url string) string {
	if len(url) <= 256*1024 {
		return url
	}
	header, data, ok := strings.Cut(url, ",")
	if !ok || !strings.HasSuffix(header, ";base64") {
		return url
	}
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return url
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		return url
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return url
	}
	if longest := max(cfg.Width, cfg.Height); longest > 1600 {
		resized := image.NewRGBA(image.Rect(0, 0, max(1, cfg.Width*1600/longest), max(1, cfg.Height*1600/longest)))
		draw.CatmullRom.Scale(resized, resized.Bounds(), img, img.Bounds(), draw.Src, nil)
		img = resized
	}
	var buf bytes.Buffer
	if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}) != nil {
		return url
	}
	encoded := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if len(encoded) >= len(url) {
		return url
	}
	return encoded
}
