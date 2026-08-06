package domain

import "testing"

func TestImageFileRef(t *testing.T) {
	vision := ImageFileRef("a/b.png", true)
	if vision != "[Image file: a/b.png - pass this path directly to image tools (e.g. ImageEdit); it cannot be opened with Read]" {
		t.Fatalf("vision ref = %q", vision)
	}
	text := ImageFileRef("a/b.png", false)
	if text != "[Image file: a/b.png - pass this path directly to image tools (e.g. ImageEdit), or to ImageDecode for a text description; it cannot be opened with Read]" {
		t.Fatalf("non-vision ref = %q", text)
	}
}

func TestImagePathNote(t *testing.T) {
	if got := ImagePathNote(ImageAttachment{Data: "aW1n"}); got != "" {
		t.Fatalf("no source path should yield no note, got %q", got)
	}

	note := ImagePathNote(ImageAttachment{DisplayName: "Image 1", SourcePath: ".infer/tmp/clipboard-image-1.png"})
	want := "[Image 1 saved at .infer/tmp/clipboard-image-1.png; use ImageDecode to inspect it if you cannot see images]"
	if note != want {
		t.Fatalf("note = %q, want %q", note, want)
	}

	if got := ImagePathNote(ImageAttachment{SourcePath: "/x.png"}); got != "[image saved at /x.png; use ImageDecode to inspect it if you cannot see images]" {
		t.Fatalf("empty display name should fall back to \"image\", got %q", got)
	}
}
