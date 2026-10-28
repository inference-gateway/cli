package infrastructure

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// MaxWorkspaceBytes caps one checkpoint's workspace payload. A workspace
// beyond it fails the transfer with a clear message instead of a stalled
// upload.
// ponytail: one size cap, resumable uploads when a workspace outgrows it.
const MaxWorkspaceBytes = 256 << 20

// detachedHead is what rev-parse answers for a detached HEAD.
const detachedHead = "HEAD"

// PackWorkspace captures a project's git state: a bundle of HEAD, a binary
// patch of the uncommitted changes, the untracked files that respect
// .gitignore and pass excluded, and the explicitly listed extra files.
func PackWorkspace(ctx context.Context, projectDir string, extra []string, excluded func(abs string) bool) (agentdomain.Workspace, error) {
	branch, refs, err := headRefs(ctx, projectDir)
	if err != nil {
		return agentdomain.Workspace{}, err
	}
	bundle, err := bundleRefs(ctx, projectDir, refs)
	if err != nil {
		return agentdomain.Workspace{}, err
	}
	patch, err := utils.RunGit(ctx, projectDir, "diff", "--binary", "HEAD")
	if err != nil {
		return agentdomain.Workspace{}, fmt.Errorf("diffing the workspace: %w", err)
	}
	untrackedPaths, err := untrackedFiles(ctx, projectDir, excluded)
	if err != nil {
		return agentdomain.Workspace{}, err
	}
	untracked, err := tarFiles(projectDir, untrackedPaths)
	if err != nil {
		return agentdomain.Workspace{}, fmt.Errorf("packing the untracked files: %w", err)
	}
	extraPaths, err := relativeFiles(projectDir, extra)
	if err != nil {
		return agentdomain.Workspace{}, err
	}
	extraTar, err := tarFiles(projectDir, extraPaths)
	if err != nil {
		return agentdomain.Workspace{}, fmt.Errorf("packing the extra files: %w", err)
	}
	ws := agentdomain.Workspace{Branch: branch, Bundle: bundle, Patch: patch, Untracked: untracked, Extra: extraTar}
	if size := len(bundle) + len(patch) + len(untracked) + len(extraTar); size > MaxWorkspaceBytes {
		return agentdomain.Workspace{}, fmt.Errorf("the workspace checkpoint is %d bytes, over the %d byte limit", size, MaxWorkspaceBytes)
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

// relativeFiles checks the extra files live inside the project and returns
// them relative to it.
func relativeFiles(projectDir string, paths []string) ([]string, error) {
	files := make([]string, 0, len(paths))
	for _, path := range paths {
		abs := path
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(projectDir, path)
		}
		rel, err := filepath.Rel(projectDir, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("extra file %q is outside the project", path)
		}
		files = append(files, rel)
	}
	return files, nil
}

// tarFiles packs the files, relative to root, as a gzipped tar. No files
// means no payload.
func tarFiles(root string, files []string) ([]byte, error) {
	if len(files) == 0 {
		return nil, nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, rel := range files {
		if err := addFile(tw, root, rel); err != nil {
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

func addFile(tw *tar.Writer, root, rel string) error {
	abs := filepath.Join(root, rel)
	info, err := os.Lstat(abs)
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
	file, err := os.Open(abs)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	_, err = io.Copy(tw, file)
	return err
}

// UnpackWorkspace restores a packed workspace into dir, which must not exist
// yet: clone the bundle, check the branch out, apply the patch only when it
// applies cleanly, then unpack the untracked and extra files.
func UnpackWorkspace(ctx context.Context, ws agentdomain.Workspace, dir string) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("workspace %s already exists", dir)
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
		if _, err := utils.RunGit(ctx, dir, "checkout", "--quiet", ws.Branch); err != nil {
			return fmt.Errorf("checking out %s: %w", ws.Branch, err)
		}
	}
	if len(ws.Patch) > 0 {
		if _, err := utils.RunGitStdin(ctx, dir, string(ws.Patch), "apply", "--binary", "--check"); err != nil {
			return fmt.Errorf("the workspace patch does not apply cleanly: %w", err)
		}
		if _, err := utils.RunGitStdin(ctx, dir, string(ws.Patch), "apply", "--binary"); err != nil {
			return fmt.Errorf("applying the workspace patch: %w", err)
		}
	}
	if err := untar(ws.Untracked, dir); err != nil {
		return fmt.Errorf("unpacking the untracked files: %w", err)
	}
	if err := untar(ws.Extra, dir); err != nil {
		return fmt.Errorf("unpacking the extra files: %w", err)
	}
	return nil
}

// untar writes a gzipped tar into dir, refusing entries that escape it.
func untar(data []byte, dir string) error {
	if len(data) == 0 {
		return nil
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
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
		target := filepath.Join(dir, filepath.FromSlash(header.Name))
		if rel, err := filepath.Rel(dir, target); err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("archive entry %q escapes the workspace", header.Name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		if err := writeEntry(target, tr, header.FileInfo().Mode().Perm()); err != nil {
			return err
		}
	}
}

func writeEntry(target string, r io.Reader, mode os.FileMode) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, r); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
