package utils

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// SeedFS writes the tree under root in fsys into destDir, mirroring its
// layout. A file is written only when absent unless overwrite is set, so the
// user's edits to a seeded file survive a re-run of `infer init`.
func SeedFS(fsys fs.FS, root, destDir string, overwrite bool) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		target := filepath.Join(destDir, strings.TrimPrefix(path, root+"/"))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !overwrite && FileExists(target) {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
