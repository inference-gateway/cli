package models

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"

	config "github.com/inference-gateway/cli/config"
	huggingface "github.com/inference-gateway/cli/internal/platform/huggingface"
)

const (
	modelsDirName = "models"
	hubDirName    = "huggingface"
)

// repoID is an "owner/name" repository id. Nothing else may name a folder of
// the library.
var repoID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*$`)

// Downloaded is a Hub repository in the library: where its files are and how
// much of it is on disk.
type Downloaded struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Dir returns the library of repositories downloaded from the Hub,
// ~/.infer/models/huggingface, one folder per owner and repository.
func Dir() string {
	return filepath.Join(config.UserSpaceConfigDir(), modelsDirName, hubDirName)
}

func repoDir(dir, id string) (string, error) {
	if !repoID.MatchString(id) {
		return "", fmt.Errorf("invalid model %q: name it as owner/name", id)
	}
	return filepath.Join(dir, filepath.FromSlash(id)), nil
}

// List returns every repository in dir that holds at least one file, sorted by
// id. A missing library is an empty one.
func List(dir string) ([]Downloaded, error) {
	folders, err := filepath.Glob(filepath.Join(dir, "*", "*"))
	if err != nil {
		return nil, fmt.Errorf("reading model library %s: %w", dir, err)
	}

	var library []Downloaded
	for _, folder := range folders {
		rel, err := filepath.Rel(dir, folder)
		if err != nil {
			return nil, err
		}
		model, err := measure(filepath.ToSlash(rel), folder)
		if err != nil {
			return nil, err
		}
		if model.Files > 0 {
			library = append(library, model)
		}
	}
	sort.Slice(library, func(i, j int) bool { return library[i].ID < library[j].ID })
	return library, nil
}

// measure counts the finished files under folder. A partial download is left
// out, since it is not usable yet.
func measure(id, folder string) (Downloaded, error) {
	model := Downloaded{ID: id, Path: folder}
	err := filepath.WalkDir(folder, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) == partialExt {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		model.Files++
		model.Bytes += info.Size()
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Downloaded{}, fmt.Errorf("reading model %s: %w", id, err)
	}
	return model, nil
}

// partialExt marks a file the downloader has not finished.
const partialExt = ".partial"

// Delete removes the repository id and all of its files from dir.
func Delete(dir, id string) error {
	folder, err := repoDir(dir, id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(folder); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("model %s is not downloaded", id)
	}
	if err := os.RemoveAll(folder); err != nil {
		return fmt.Errorf("deleting model %s: %w", id, err)
	}
	return nil
}

// Download fetches the repository id from the Hub into dir and returns what is
// on disk afterwards. only names the files to fetch, all of them when empty.
// A file that is already there is kept, so a stopped download resumes per file.
func Download(ctx context.Context, hub *huggingface.Client, dir, id string, only []string, report func(string)) (Downloaded, error) {
	folder, err := repoDir(dir, id)
	if err != nil {
		return Downloaded{}, err
	}
	repo := huggingface.Repo{ID: id}

	listed, err := hub.Files(ctx, repo)
	if err != nil {
		return Downloaded{}, err
	}
	wanted, err := chosen(id, listed, only)
	if err != nil {
		return Downloaded{}, err
	}

	for _, file := range wanted {
		target := filepath.Join(folder, filepath.FromSlash(file.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Downloaded{}, fmt.Errorf("creating %s: %w", filepath.Dir(target), err)
		}
		if _, err := hub.EnsureFile(ctx, repo, file.Path, folder, file.Path, true, report); err != nil {
			return Downloaded{}, err
		}
	}
	return measure(id, folder)
}

// chosen returns the files of a repository to fetch: every one of them, or the
// ones only names. A path that could leave the repository folder is refused.
func chosen(id string, listed []huggingface.File, only []string) ([]huggingface.File, error) {
	for _, name := range only {
		if !slices.ContainsFunc(listed, func(f huggingface.File) bool { return f.Path == name }) {
			return nil, fmt.Errorf("model %s has no file %q", id, name)
		}
	}

	var wanted []huggingface.File
	for _, file := range listed {
		if len(only) > 0 && !slices.Contains(only, file.Path) {
			continue
		}
		if !filepath.IsLocal(filepath.FromSlash(file.Path)) {
			return nil, fmt.Errorf("model %s lists an unsafe file path %q", id, file.Path)
		}
		wanted = append(wanted, file)
	}
	if len(wanted) == 0 {
		return nil, fmt.Errorf("model %s has no files to download", id)
	}
	return wanted, nil
}
