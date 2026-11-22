package utils

import (
	"io/fs"
	"os"
	"path/filepath"
)

// WriteFileAtomic replaces path with data through a synced temp file in the
// same directory and a rename, so a concurrent reader sees the old file or the
// whole new one, never a torn one. The file ends up with mode perm, and the
// temp file is removed on every failure, a failed rename included.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
