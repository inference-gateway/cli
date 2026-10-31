package infrastructure

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// MaxRepositoryBytes caps one checkpoint's repository payload. A repository
// beyond it fails the teleport with a clear message instead of a stalled
// upload.
// ponytail: one size cap, resumable uploads when a repository outgrows it.
const MaxRepositoryBytes = 256 << 20

// detachedHead is what rev-parse answers for a detached HEAD.
const detachedHead = "HEAD"

// PackRepository captures a project's git state: a bundle of HEAD, a binary
// patch of the uncommitted changes, the untracked files that respect
// .gitignore and pass excluded, and the explicitly listed extra files.
func PackRepository(ctx context.Context, projectDir string, extra []string, excluded func(abs string) bool) (agentdomain.Repository, error) {
	branch, refs, err := headRefs(ctx, projectDir)
	if err != nil {
		return agentdomain.Repository{}, err
	}
	bundle, err := bundleRefs(ctx, projectDir, refs)
	if err != nil {
		return agentdomain.Repository{}, err
	}
	patch, err := utils.RunGit(ctx, projectDir, "diff", "--binary", "HEAD")
	if err != nil {
		return agentdomain.Repository{}, fmt.Errorf("diffing the repository: %w", err)
	}
	if err := denyUncommitted(ctx, projectDir, excluded); err != nil {
		return agentdomain.Repository{}, err
	}
	untrackedPaths, err := untrackedFiles(ctx, projectDir, excluded)
	if err != nil {
		return agentdomain.Repository{}, err
	}
	untracked, err := tarFiles(projectDir, untrackedPaths)
	if err != nil {
		return agentdomain.Repository{}, fmt.Errorf("packing the untracked files: %w", err)
	}
	extraPaths, err := relativeFiles(projectDir, extra, excluded)
	if err != nil {
		return agentdomain.Repository{}, err
	}
	extraTar, err := tarFiles(projectDir, extraPaths)
	if err != nil {
		return agentdomain.Repository{}, fmt.Errorf("packing the extra files: %w", err)
	}
	ws := agentdomain.Repository{Branch: branch, Origin: originURL(ctx, projectDir), Bundle: bundle, Patch: patch, Untracked: untracked, Extra: extraTar}
	if size := len(bundle) + len(patch) + len(untracked) + len(extraTar); size > MaxRepositoryBytes {
		return agentdomain.Repository{}, fmt.Errorf("the repository checkpoint is %d bytes, over the %d byte limit", size, MaxRepositoryBytes)
	}
	return ws, nil
}

// headRefs names the checked-out branch, or the commit when HEAD is detached,
// and the refs the bundle carries so a clone lands on the same spot.
func headRefs(ctx context.Context, projectDir string) (string, []string, error) {
	out, err := utils.RunGit(ctx, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", nil, fmt.Errorf("the project is not a git checkout with a commit: %w", err)
	}
	branch := strings.TrimSpace(string(out))
	if branch != detachedHead {
		return branch, []string{detachedHead, branch}, nil
	}
	sha, err := utils.RunGit(ctx, projectDir, "rev-parse", detachedHead)
	if err != nil {
		return "", nil, fmt.Errorf("resolving HEAD: %w", err)
	}
	return strings.TrimSpace(string(sha)), []string{detachedHead}, nil
}

// originURL is the project's origin remote, with the userinfo of an http(s)
// URL dropped so a token embedded there never travels. No origin is "".
func originURL(ctx context.Context, projectDir string) string {
	out, err := utils.RunGit(ctx, projectDir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	origin := strings.TrimSpace(string(out))
	if u, err := url.Parse(origin); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		u.User = nil
		return u.String()
	}
	return origin
}

func bundleRefs(ctx context.Context, projectDir string, refs []string) ([]byte, error) {
	file, err := os.CreateTemp("", "infer-bundle-*.bundle")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	_ = file.Close()
	defer func() { _ = os.Remove(path) }()
	if _, err := utils.RunGit(ctx, projectDir, append([]string{"bundle", "create", path}, refs...)...); err != nil {
		return nil, fmt.Errorf("bundling the repository: %w", err)
	}
	return os.ReadFile(path)
}

// untrackedFiles lists the files git ignores nothing about yet does not track,
// minus the ones the sandbox policy denies.
func untrackedFiles(ctx context.Context, projectDir string, excluded func(abs string) bool) ([]string, error) {
	out, err := utils.RunGit(ctx, projectDir, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, fmt.Errorf("listing the untracked files: %w", err)
	}
	var files []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		if excluded != nil && excluded(filepath.Join(projectDir, rel)) {
			continue
		}
		files = append(files, rel)
	}
	return files, nil
}

// denyUncommitted refuses the checkpoint when a tracked file the sandbox
// policy denies has uncommitted changes, since the patch would ship its
// exact bytes. It fails loudly instead of trimming the patch, which would
// no longer apply.
func denyUncommitted(ctx context.Context, projectDir string, excluded func(abs string) bool) error {
	if excluded == nil {
		return nil
	}
	out, err := utils.RunGit(ctx, projectDir, "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		return fmt.Errorf("listing the uncommitted changes: %w", err)
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" && excluded(filepath.Join(projectDir, rel)) {
			return fmt.Errorf("the uncommitted changes touch %q, which the sandbox policy denies", rel)
		}
	}
	return nil
}

// relativeFiles checks the extra files live inside the project and pass
// excluded, and returns them relative to it.
func relativeFiles(projectDir string, paths []string, excluded func(abs string) bool) ([]string, error) {
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(projectDir, path)
		}
		rel, err := filepath.Rel(projectDir, abs)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("extra file %q is outside the project", path)
		}
		if excluded != nil && excluded(abs) {
			return nil, fmt.Errorf("extra file %q is denied by the sandbox policy", path)
		}
		files = append(files, rel)
	}
	return files, nil
}

// tarFiles packs the files, relative to projectDir, as a gzipped tar. Files
// are read through an os.Root, so a symlinked directory never leads outside
// the project. No files means no payload.
func tarFiles(projectDir string, files []string) ([]byte, error) {
	if len(files) == 0 {
		return nil, nil
	}
	project, err := os.OpenRoot(projectDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = project.Close() }()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, rel := range files {
		if err := addFile(tw, project, rel); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func addFile(tw *tar.Writer, project *os.Root, rel string) error {
	info, err := project.Lstat(rel)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(rel)
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	file, err := project.Open(rel)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = io.Copy(tw, file)
	return err
}

// UnpackRepository restores a packed repository into dir, which must not exist
// yet: clone the bundle, check the branch out, apply the patch only when it
// applies cleanly, then unpack the untracked and extra files. The denied
// callback keeps a peer-planted entry out, the way the sender's policy does.
func UnpackRepository(ctx context.Context, ws agentdomain.Repository, dir string, denied func(rel string) bool) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("workspace dir %s already exists", dir)
	}
	file, err := os.CreateTemp("", "infer-bundle-*.bundle")
	if err != nil {
		return err
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := file.Write(ws.Bundle); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := utils.RunGit(ctx, "", "clone", "--quiet", path, dir); err != nil {
		return fmt.Errorf("cloning the bundle: %w", err)
	}
	if ws.Branch != "" {
		if strings.HasPrefix(ws.Branch, "-") {
			return fmt.Errorf("the repository branch %q is not a branch or a commit", ws.Branch)
		}
		if _, err := utils.RunGit(ctx, dir, "checkout", "--quiet", ws.Branch); err != nil {
			return fmt.Errorf("checking out %s: %w", ws.Branch, err)
		}
	}
	if err := pointOrigin(ctx, dir, ws.Origin); err != nil {
		return err
	}
	if len(ws.Patch) > 0 {
		if _, err := utils.RunGitStdin(ctx, dir, string(ws.Patch), "apply", "--binary", "--check"); err != nil {
			return fmt.Errorf("the repository patch does not apply cleanly: %w", err)
		}
		if _, err := utils.RunGitStdin(ctx, dir, string(ws.Patch), "apply", "--binary"); err != nil {
			return fmt.Errorf("applying the repository patch: %w", err)
		}
	}
	if err := untar(ws.Untracked, dir, denied, MaxRepositoryBytes); err != nil {
		return fmt.Errorf("unpacking the untracked files: %w", err)
	}
	if err := untar(ws.Extra, dir, denied, MaxRepositoryBytes); err != nil {
		return fmt.Errorf("unpacking the extra files: %w", err)
	}
	return nil
}

// pointOrigin aims the clone's origin, which names the deleted bundle file, at
// the source's remote, or removes it when the source had none. The "--" keeps
// a peer-sent URL from reading as an option.
func pointOrigin(ctx context.Context, dir, origin string) error {
	args := []string{"remote", "remove", "origin"}
	if origin != "" {
		args = []string{"remote", "set-url", "--", "origin", origin}
	}
	if _, err := utils.RunGit(ctx, dir, args...); err != nil {
		return fmt.Errorf("pointing origin at the source's remote: %w", err)
	}
	return nil
}

// untar writes a gzipped tar into dir through an os.Root, refusing entries
// that name a path outside it or reach outside it through a symlink, entries
// under .git, entries the denied callback refuses, and an archive whose
// decompressed size passes limit.
func untar(data []byte, dir string, denied func(rel string) bool, limit int64) error {
	if len(data) == 0 {
		return nil
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(&limitedReader{r: gz, left: limit, limit: limit})
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.Clean(filepath.FromSlash(header.Name))
		if !filepath.IsLocal(name) {
			return fmt.Errorf("archive entry %q escapes the workspace dir", header.Name)
		}
		if rejectedEntry(filepath.ToSlash(name), denied) {
			return fmt.Errorf("archive entry %q is not allowed into the workspace dir", header.Name)
		}
		if err := root.MkdirAll(filepath.Dir(name), 0755); err != nil {
			return fmt.Errorf("archive entry %q: %w", header.Name, err)
		}
		if err := writeEntry(root, name, tr, min(header.FileInfo().Mode().Perm(), 0755)); err != nil {
			return fmt.Errorf("archive entry %q: %w", header.Name, err)
		}
	}
}

// rejectedEntry reports whether an archive entry may not be restored: it sits
// under .git, where a planted config or hook would run git's code on the next
// command, or the policy denies it.
func rejectedEntry(rel string, denied func(rel string) bool) bool {
	first, _, _ := strings.Cut(rel, "/")
	if strings.EqualFold(first, ".git") {
		return true
	}
	return denied != nil && denied(rel)
}

// limitedReader fails once reading passes limit, so a small compressed
// archive cannot decompress into an unbounded workspace dir.
type limitedReader struct {
	r     io.Reader
	left  int64
	limit int64
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.left <= 0 {
		return 0, fmt.Errorf("the archive decompresses past %d bytes", l.limit)
	}
	if int64(len(p)) > l.left {
		p = p[:l.left]
	}
	n, err := l.r.Read(p)
	l.left -= int64(n)
	return n, err
}

func writeEntry(root *os.Root, name string, r io.Reader, mode os.FileMode) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
