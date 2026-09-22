package avatars

import (
	"context"
	"errors"
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
	if primary := got[1].Primary(dir); primary != filepath.Join(dir, "presenter", "01-front.png") {
		t.Errorf("Primary = %q", primary)
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
	writeLibrary(t, filepath.Dir(photo), "me.JPG")

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
		if avatar.Primary(dir) != filepath.Join(dir, "presenter", "01-front.jpg") {
			t.Fatalf("primary = %q", avatar.Primary(dir))
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
