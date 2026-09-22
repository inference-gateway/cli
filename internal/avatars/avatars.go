// Package avatars manages the avatar library: ~/.infer/avatars/<name>/, one
// folder per avatar holding one or more portrait images of the same person
// (e.g. shots from different angles). The TextToVideo tool renders
// lip-synced clips from an avatar's primary image; `infer avatars` lists and
// deletes them.
package avatars

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
)

// ImageExtensions are the portrait formats an avatar folder may hold.
var ImageExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// Avatar is one library entry. Images are the folder's portrait file names in
// sorted order; the first is the primary image lip-sync models receive.
type Avatar struct {
	Name   string   `json:"name"`
	Images []string `json:"images"`
}

// Primary returns the path of the avatar's primary image inside dir.
func (a Avatar) Primary(dir string) string {
	return filepath.Join(dir, a.Name, a.Images[0])
}

// Dir returns the avatar library directory, ~/.infer/avatars.
func Dir() string {
	return filepath.Join(config.UserSpaceConfigDir(), "avatars")
}

// List returns every avatar in dir that holds at least one image, sorted by
// name. A missing library is an empty one.
func List(dir string) ([]Avatar, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading avatar library %s: %w", dir, err)
	}
	var out []Avatar
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if avatar, err := read(dir, entry.Name()); err == nil {
			out = append(out, avatar)
		}
	}
	return out, nil
}

// Get returns the named avatar from dir. The name must be a bare folder name.
func Get(dir, name string) (Avatar, error) {
	if err := validName(name); err != nil {
		return Avatar{}, err
	}
	return read(dir, name)
}

// Delete removes the named avatar folder and every image in it.
func Delete(dir, name string) error {
	if _, err := Get(dir, name); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("deleting avatar %q: %w", name, err)
	}
	return nil
}

// Names returns the names of the avatars in dir, for error hints.
func Names(dir string) []string {
	list, _ := List(dir)
	names := make([]string, 0, len(list))
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names
}

func read(dir, name string) (Avatar, error) {
	entries, err := os.ReadDir(filepath.Join(dir, name))
	if err != nil {
		return Avatar{}, fmt.Errorf("avatar %q not found in %s", name, dir)
	}
	avatar := Avatar{Name: name}
	for _, entry := range entries {
		if entry.Type().IsRegular() && slices.Contains(ImageExtensions, strings.ToLower(filepath.Ext(entry.Name()))) {
			avatar.Images = append(avatar.Images, entry.Name())
		}
	}
	if len(avatar.Images) == 0 {
		return Avatar{}, fmt.Errorf("avatar %q in %s holds no .png, .jpg, .jpeg or .webp image", name, dir)
	}
	return avatar, nil // os.ReadDir sorts by file name
}

func validName(name string) error {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return fmt.Errorf("invalid avatar name %q: pass a bare avatar name", name)
	}
	return nil
}
