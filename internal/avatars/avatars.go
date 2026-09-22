// Package avatars manages the avatar library: ~/.infer/avatars/<name>/, one
// folder per avatar holding one or more portrait images of the same person
// (e.g. shots from different angles). The TextToVideo tool renders
// lip-synced clips from an avatar's primary image; `infer avatars` creates,
// lists and deletes them.
package avatars

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	config "github.com/inference-gateway/cli/config"
)

// ImageExtensions are the portrait formats an avatar folder may hold.
var ImageExtensions = []string{".png", ".jpg", ".jpeg", ".webp"}

// Angles maps each generatable view to the image-edit prompt that produces it
// from the front photo. Prompts ask for a neutral, closed mouth so the shots
// suit lip-sync models.
var Angles = map[string]string{
	"three-quarter-left":  anglePrompt("a three-quarter view, head and shoulders turned about 45 degrees to their left"),
	"three-quarter-right": anglePrompt("a three-quarter view, head and shoulders turned about 45 degrees to their right"),
	"left-profile":        anglePrompt("a full side profile facing left"),
	"right-profile":       anglePrompt("a full side profile facing right"),
}

// DefaultAngles are generated when the caller does not choose: the two
// three-quarter views hold identity better than full profiles.
var DefaultAngles = []string{"three-quarter-left", "three-quarter-right"}

func anglePrompt(view string) string {
	return "Photograph of the same person from " + view + ". Keep their identity, facial features, " +
		"skin tone, hair, clothing, lighting and background exactly the same. Neutral expression, " +
		"mouth closed, looking natural. Photorealistic, sharp focus, no text or watermark."
}

// EditFunc edits the image at imagePath with prompt and returns the path of
// the generated image file.
type EditFunc func(ctx context.Context, prompt, imagePath string) (string, error)

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

// Get returns the named avatar from dir. The name must be a bare folder
// name: no absolute path, separators or "..", and not "." (the library
// itself). The check stays inline, in the shape the TTS input helper uses, so
// static analysis sees the name sanitized before it reaches the filesystem.
func Get(dir, name string) (Avatar, error) {
	if name == "" || name == "." || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return Avatar{}, fmt.Errorf("invalid avatar name %q: pass a bare avatar name", name)
	}
	return read(dir, filepath.Base(name))
}

// Delete removes the named avatar folder and every image in it.
func Delete(dir, name string) error {
	avatar, err := Get(dir, name)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Join(dir, avatar.Name)); err != nil {
		return fmt.Errorf("deleting avatar %q: %w", avatar.Name, err)
	}
	return nil
}

// Create builds a new avatar folder dir/<name>/ from a front photo: the photo
// is copied in as the primary image (01-front.<ext>) and each angle is
// generated from it concurrently with edit and saved as
// NN-<angle>.png in the order given. An existing avatar is never overwritten,
// and on any failure the half-built folder is removed.
func Create(ctx context.Context, dir, name, photo string, angles []string, edit EditFunc) (Avatar, error) {
	if name == "" || name == "." || filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return Avatar{}, fmt.Errorf("invalid avatar name %q: pass a bare avatar name", name)
	}
	ext := strings.ToLower(filepath.Ext(photo))
	if !slices.Contains(ImageExtensions, ext) {
		return Avatar{}, fmt.Errorf("photo %q must be a .png, .jpg, .jpeg or .webp image", photo)
	}
	for _, angle := range angles {
		if _, ok := Angles[angle]; !ok {
			return Avatar{}, fmt.Errorf("unknown angle %q (choose from %s)", angle, strings.Join(slices.Sorted(maps.Keys(Angles)), ", "))
		}
	}
	front, err := os.ReadFile(photo) // nolint:gosec // the caller's own file, named on the command line
	if err != nil {
		return Avatar{}, fmt.Errorf("reading photo: %w", err)
	}

	folder := filepath.Join(dir, filepath.Base(name))
	if _, err := os.Stat(folder); err == nil {
		return Avatar{}, fmt.Errorf("avatar %q already exists; delete it first", name)
	}
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return Avatar{}, fmt.Errorf("creating avatar folder: %w", err)
	}
	if err := generate(ctx, folder, ext, front, photo, angles, edit); err != nil {
		_ = os.RemoveAll(folder)
		return Avatar{}, err
	}
	return read(dir, filepath.Base(name))
}

// generate writes the front photo and every generated angle into folder.
// Angles run concurrently - each goroutine owns one slot of errs - and the
// first failure does not stop the others, so the error lists every failed
// angle.
func generate(ctx context.Context, folder, ext string, front []byte, photo string, angles []string, edit EditFunc) error {
	if err := os.WriteFile(filepath.Join(folder, "01-front"+ext), front, 0o644); err != nil { // nolint:gosec
		return fmt.Errorf("saving front photo: %w", err)
	}
	errs := make([]error, len(angles))
	var wg sync.WaitGroup
	for i, angle := range angles {
		wg.Go(func() {
			generated, err := edit(ctx, Angles[angle], photo)
			if err == nil {
				err = move(generated, filepath.Join(folder, fmt.Sprintf("%02d-%s.png", i+2, angle)))
			}
			if err != nil {
				errs[i] = fmt.Errorf("generating %s view: %w", angle, err)
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// move relocates a generated image into the avatar folder. It copies then
// removes, since the image service may save on another filesystem.
func move(src, dst string) error {
	data, err := os.ReadFile(src) // nolint:gosec // path returned by the image service
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil { // nolint:gosec
		return err
	}
	_ = os.Remove(src)
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
