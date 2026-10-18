package loop

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"

	sdk "github.com/inference-gateway/sdk"
)

func TestRequestImagesKeepsRecentImagesAndDoesNotMutateHistory(t *testing.T) {
	old, _ := sdk.NewImageContentPart("data:image/png;base64,"+strings.Repeat("A", 3<<19), nil)
	latest, _ := sdk.NewImageContentPart("data:image/png;base64,"+strings.Repeat("B", 3<<19), nil)
	text, _ := sdk.NewTextContentPart("Implement the passage")
	messages := []sdk.Message{
		{Role: sdk.User, Content: sdk.NewMessageContent([]sdk.ContentPart{text, old})},
		{Role: sdk.User, Content: sdk.NewMessageContent([]sdk.ContentPart{text, latest})},
	}
	before, _ := json.Marshal(messages)
	out := requestImages(messages)
	after, _ := json.Marshal(messages)
	if !bytes.Equal(before, after) {
		t.Fatal("persisted history mutated")
	}
	parts, _ := out[0].Content.AsMessageContent1()
	note, err := parts[1].AsTextContentPart()
	if err != nil || !strings.Contains(note.Text, "Image omitted") {
		t.Fatal("old image not replaced by explicit note")
	}
	parts, _ = out[1].Content.AsMessageContent1()
	got, err := parts[1].AsImageContentPart()
	if err != nil || len(got.ImageURL.URL) < 3<<19 {
		t.Fatal("latest image lost")
	}
}

func TestCompactRequestImageReducesLargeScreenshots(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2000, 1000))
	random := rand.New(rand.NewPCG(1, 2))
	for y := 0; y < 1000; y++ {
		for x := 0; x < 2000; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(random.Uint32()), uint8(random.Uint32()), uint8(random.Uint32()), 255})
		}
	}
	var source bytes.Buffer
	if err := png.Encode(&source, img); err != nil {
		t.Fatal(err)
	}
	original := "data:image/png;base64," + base64.StdEncoding.EncodeToString(source.Bytes())
	compact := compactRequestImage(original)
	if len(compact) >= len(original) || !strings.HasPrefix(compact, "data:image/jpeg;base64,") {
		t.Fatal("screenshot not compressed")
	}
	_, data, _ := strings.Cut(compact, ",")
	raw, _ := base64.StdEncoding.DecodeString(data)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 1600 || cfg.Height != 800 {
		t.Fatalf("wrong screenshot dimensions: %+v %v", cfg, err)
	}
}
