package avatars

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// writeLibrary lays out files (paths relative to dir) as empty files.
func writeLibrary(t *testing.T, dir string, files ...string) {
	t.Helper()
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	writeLibrary(t, dir,
		"presenter/02-left.JPG", "presenter/01-front.png", "presenter/notes.txt",
		"host/portrait.webp",
		"empty/readme.md",
		"stray.png",
	)

	got, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []Avatar{
		{Name: "host", Images: []string{"portrait.webp"}},
		{Name: "presenter", Images: []string{"01-front.png", "02-left.JPG"}},
	}
	if !slices.EqualFunc(got, want, func(a, b Avatar) bool { return a.Name == b.Name && slices.Equal(a.Images, b.Images) }) {
		t.Fatalf("List = %+v, want %+v", got, want)
	}

	missing, err := List(filepath.Join(dir, "nope"))
	if err != nil || len(missing) != 0 {
		t.Errorf("List(missing) = %v, %v; want empty, nil", missing, err)
	}
}

func TestGet(t *testing.T) {
	dir := t.TempDir()
	writeLibrary(t, dir, "presenter/front.png", "empty/readme.md")

	tests := []struct {
		name    string
		avatar  string
		wantErr bool
	}{
		{"existing", "presenter", false},
		{"missing", "ghost", true},
		{"no images", "empty", true},
		{"traversal", "../presenter", true},
		{"nested", "presenter/front.png", true},
		{"absolute", filepath.Join(dir, "presenter"), true},
		{"dot dot", "..", true},
		{"library root", ".", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Get(dir, tt.avatar)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Get(%q) err = %v, wantErr %v", tt.avatar, err, tt.wantErr)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	writeLibrary(t, dir, "presenter/front.png", "presenter/side.jpg", "host/portrait.webp")

	if err := Delete(dir, "../host"); err == nil {
		t.Fatal("Delete accepted a traversal name")
	}
	if err := Delete(dir, "."); err == nil {
		t.Fatal("Delete accepted the library root")
	}
	if err := Delete(dir, "ghost"); err == nil {
		t.Fatal("Delete accepted a missing avatar")
	}
	if err := Delete(dir, "presenter"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "presenter")); !os.IsNotExist(err) {
		t.Fatalf("presenter folder still exists: %v", err)
	}
	if names := Names(dir); !slices.Equal(names, []string{"host"}) {
		t.Fatalf("Names after delete = %v, want [host]", names)
	}
}

// fakeEdit returns an EditFunc that writes a generated PNG into scratch and
// records the prompts it saw; prompts in fail make it return an error.
func fakeEdit(t *testing.T, fail ...string) (EditFunc, *[]string) {
	t.Helper()
	scratch := t.TempDir()
	var mu sync.Mutex
	var prompts []string
	return func(_ context.Context, prompt, imagePath string) (string, error) {
		if _, err := os.Stat(imagePath); err != nil {
			return "", err
		}
		mu.Lock()
		prompts = append(prompts, prompt)
		n := len(prompts)
		mu.Unlock()
		if slices.Contains(fail, prompt) {
			return "", errors.New("provider refused")
		}
		out := filepath.Join(scratch, strings.Repeat("x", n)+".png")
		return out, os.WriteFile(out, []byte("generated"), 0o600)
	}, &prompts
}

func TestCreate(t *testing.T) {
	photo := filepath.Join(t.TempDir(), "me.JPG")
	writeJPEG(t, photo, binary.BigEndian, 0)

	t.Run("copies the front photo and generates each angle in order", func(t *testing.T) {
		dir := t.TempDir()
		edit, prompts := fakeEdit(t)

		avatar, err := Create(context.Background(), dir, "presenter", photo, DefaultAngles, edit)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		want := []string{"01-front.jpg", "02-three-quarter-left.png", "03-three-quarter-right.png"}
		if !slices.Equal(avatar.Images, want) {
			t.Fatalf("Images = %v, want %v", avatar.Images, want)
		}
		if len(*prompts) != 2 || !slices.Contains(*prompts, Angles["three-quarter-left"]) || !slices.Contains(*prompts, Angles["three-quarter-right"]) {
			t.Fatalf("edit prompts = %v", *prompts)
		}
		if avatar.Images[0] != "01-front.jpg" {
			t.Fatalf("primary = %q", avatar.Images[0])
		}
	})

	t.Run("no angles only copies the photo", func(t *testing.T) {
		dir := t.TempDir()
		edit, prompts := fakeEdit(t)
		avatar, err := Create(context.Background(), dir, "presenter", photo, nil, edit)
		if err != nil || !slices.Equal(avatar.Images, []string{"01-front.jpg"}) || len(*prompts) != 0 {
			t.Fatalf("Create = %+v, %v (prompts %v)", avatar, err, *prompts)
		}
	})

	t.Run("a failed angle removes the half-built avatar", func(t *testing.T) {
		dir := t.TempDir()
		edit, _ := fakeEdit(t, Angles["three-quarter-right"])
		_, err := Create(context.Background(), dir, "presenter", photo, DefaultAngles, edit)
		if err == nil || !strings.Contains(err.Error(), "three-quarter-right") {
			t.Fatalf("Create err = %v, want the failed angle named", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "presenter")); !os.IsNotExist(statErr) {
			t.Fatalf("half-built avatar left behind: %v", statErr)
		}
	})

	t.Run("an existing avatar is never overwritten", func(t *testing.T) {
		dir := t.TempDir()
		writeLibrary(t, dir, "presenter/01-front.png")
		edit, prompts := fakeEdit(t)
		if _, err := Create(context.Background(), dir, "presenter", photo, DefaultAngles, edit); err == nil || !strings.Contains(err.Error(), "already exists") {
			t.Fatalf("Create err = %v, want already exists", err)
		}
		if len(*prompts) != 0 {
			t.Fatal("edit called for an existing avatar")
		}
	})

	for _, tt := range []struct {
		name, avatar, photo string
		angles              []string
		wantErr             string
	}{
		{"traversal name", "../x", photo, nil, "invalid avatar name"},
		{"library root", ".", photo, nil, "invalid avatar name"},
		{"unsupported photo", "presenter", filepath.Join(filepath.Dir(photo), "me.gif"), nil, "must be a .png"},
		{"missing photo", "presenter", filepath.Join(filepath.Dir(photo), "nope.png"), nil, "reading photo"},
		{"unknown angle", "presenter", photo, []string{"upside-down"}, "unknown angle"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			edit, prompts := fakeEdit(t)
			_, err := Create(context.Background(), dir, tt.avatar, tt.photo, tt.angles, edit)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Create err = %v, want %q", err, tt.wantErr)
			}
			if len(*prompts) != 0 {
				t.Fatal("edit called for a rejected request")
			}
		})
	}
}

// TestCreateNormalizesPhoto covers a sideways phone photo: stored upright
// without metadata, and the angles are edited from that stored image.
func TestCreateNormalizesPhoto(t *testing.T) {
	dir := t.TempDir()
	sideways := filepath.Join(t.TempDir(), "phone.jpeg")
	writeJPEG(t, sideways, binary.BigEndian, 6)
	var edited []string
	var mu sync.Mutex
	edit := func(_ context.Context, _, imagePath string) (string, error) {
		mu.Lock()
		edited = append(edited, imagePath)
		mu.Unlock()
		out := filepath.Join(t.TempDir(), "gen.png")
		return out, os.WriteFile(out, []byte("generated"), 0o600)
	}

	if _, err := Create(context.Background(), dir, "presenter", sideways, DefaultAngles, edit); err != nil {
		t.Fatalf("Create: %v", err)
	}
	front := filepath.Join(dir, "presenter", "01-front.jpeg")
	data, err := os.ReadFile(front)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("Exif")) || bytes.Contains(data, []byte("GPS-SECRET")) {
		t.Fatal("stored photo still carries EXIF metadata")
	}
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 8 || b.Dy() != 16 {
		t.Fatalf("stored size = %v, want 8x16 (rotated upright)", b.Size())
	}
	// Orientation 6 turns the stored left (red) half into the top.
	if r, _, bl, _ := img.At(4, 3).RGBA(); r <= bl {
		t.Fatal("top of the upright photo is not the red half")
	}
	if r, _, bl, _ := img.At(4, 12).RGBA(); bl <= r {
		t.Fatal("bottom of the upright photo is not the blue half")
	}
	if len(edited) != 2 || edited[0] != front || edited[1] != front {
		t.Fatalf("angles edited from %v, want the stored front %s", edited, front)
	}
}

// writeJPEG writes a 16x8 JPEG, left half red and right half blue. A non-zero
// orientation adds an APP1 EXIF segment in the given byte order carrying that
// Orientation tag plus a GPS-like marker string.
func writeJPEG(t *testing.T, path string, order binary.AppendByteOrder, orientation uint16) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 8))
	for y := range 8 {
		for x := range 16 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 8 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	if orientation != 0 {
		data = slices.Concat(data[:2], exifSegment(order, orientation), data[2:])
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// exifSegment builds an APP1 EXIF segment whose first IFD holds one
// Orientation entry.
func exifSegment(order binary.AppendByteOrder, orientation uint16) []byte {
	tiff := []byte("MM")
	if order == binary.LittleEndian {
		tiff = []byte("II")
	}
	tiff = order.AppendUint16(tiff, 42)
	tiff = order.AppendUint32(tiff, 8)
	tiff = order.AppendUint16(tiff, 1)           // one IFD entry
	tiff = order.AppendUint16(tiff, 0x0112)      // Orientation
	tiff = order.AppendUint16(tiff, 3)           // SHORT
	tiff = order.AppendUint32(tiff, 1)           // count
	tiff = order.AppendUint16(tiff, orientation) // value, left-justified
	tiff = order.AppendUint16(tiff, 0)
	tiff = order.AppendUint32(tiff, 0) // no next IFD
	tiff = append(tiff, "GPS-SECRET"...)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	return append(binary.BigEndian.AppendUint16([]byte{0xFF, 0xE1}, uint16(len(payload)+2)), payload...)
}

func TestExifOrientation(t *testing.T) {
	jpegWith := func(seg []byte) []byte { return slices.Concat([]byte{0xFF, 0xD8}, seg, []byte{0xFF, 0xDA, 0, 2}) }
	for _, tt := range []struct {
		name string
		jpg  []byte
		want int
	}{
		{"big-endian (iPhone)", jpegWith(exifSegment(binary.BigEndian, 6)), 6},
		{"little-endian", jpegWith(exifSegment(binary.LittleEndian, 8)), 8},
		{"no EXIF", jpegWith(nil), 1},
		{"out-of-range value", jpegWith(exifSegment(binary.BigEndian, 9)), 1},
		{"truncated segment", []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x10, 0x00}, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := exifOrientation(tt.jpg); got != tt.want {
				t.Fatalf("exifOrientation = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOrient(t *testing.T) {
	// Stored 2x2 image [A B; C D] and how each EXIF orientation displays it.
	const a, b, c, d = 10, 20, 30, 40
	src := image.NewGray(image.Rect(0, 0, 2, 2))
	src.Pix = []uint8{a, b, c, d}
	for o, want := range map[int][4]uint8{
		1: {a, b, c, d},
		2: {b, a, d, c},
		3: {d, c, b, a},
		4: {c, d, a, b},
		5: {a, c, b, d},
		6: {c, a, d, b},
		7: {d, b, c, a},
		8: {b, d, a, c},
	} {
		img := orient(src, o)
		var got [4]uint8
		for i := range 4 {
			got[i] = color.GrayModel.Convert(img.At(i%2, i/2)).(color.Gray).Y
		}
		if got != want {
			t.Errorf("orientation %d = %v, want %v", o, got, want)
		}
	}
}
