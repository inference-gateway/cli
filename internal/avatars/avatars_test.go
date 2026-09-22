package avatars

import (
	"os"
	"path/filepath"
	"slices"
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
