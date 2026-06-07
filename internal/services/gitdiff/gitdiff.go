// Package gitdiff is a small, focused data layer behind the `/diff` changes
// panel. It shells out to the git CLI (mirroring the exec.Command("git", ...)
// style used elsewhere in the codebase) to list working-tree changes and to
// fetch the before/after content for a single changed file.
//
// It deliberately returns raw before/after content rather than a preformatted
// patch: the UI renders the diff via the diffview package, which computes the
// line diff itself. The Source interface lives here (not in internal/domain) so
// it does not trigger counterfeiter mock regeneration - the only consumer is
// the diff viewer component, which can be tested with a hand-written fake.
package gitdiff

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxRenderBytes caps how large a single side of a diff may be before the
// viewer treats it as "not shown" (alongside true binary files). Keeps the
// line-diff algorithm from churning on huge generated/lock files.
const maxRenderBytes = 1 << 20 // 1 MiB

// Status is a single-letter git status code for a changed file.
type Status rune

const (
	StatusModified   Status = 'M'
	StatusAdded      Status = 'A'
	StatusDeleted    Status = 'D'
	StatusRenamed    Status = 'R'
	StatusCopied     Status = 'C'
	StatusTypeChange Status = 'T'
	StatusUnmerged   Status = 'U'
	StatusUntracked  Status = '?'
)

// FileChange describes one changed path, in either the staged (index) group or
// the unstaged (working-tree) group. A path with both staged and unstaged edits
// (porcelain "MM") yields one FileChange in each group, like VS Code.
type FileChange struct {
	Path     string // working-tree path (the new path for renames)
	OrigPath string // rename/copy source path, else ""
	Status   Status
	Staged   bool // true → index/staged group, false → working-tree group
}

// Hunk is one "@@ ... @@" section of a unified diff for a file.
type Hunk struct {
	Header string   // the "@@ -a,b +c,d @@" line (plus any trailing section heading)
	Lines  []string // body lines, each prefixed with ' ', '+', '-' or '\'
}

// FilePatch is a single file's unified diff, split into its preamble (the
// "diff --git"/"index"/"---"/"+++" lines) and hunks. It is the unit operated on
// by hunk-level (git add -p style) staging.
type FilePatch struct {
	Preamble string
	Hunks    []Hunk
}

// Source provides the data behind the changes panel.
type Source interface {
	// Changes returns the staged and unstaged file groups.
	Changes() (staged, unstaged []FileChange, err error)
	// Diff returns the before/after content for a change. Refs are chosen by
	// group: a staged entry compares HEAD→index, an unstaged entry compares
	// index(→HEAD fallback)→working tree. Added/untracked → old "", deleted →
	// new "". isBinary is true for binary or oversized content (skip diffing).
	Diff(fc FileChange) (oldContent, newContent string, isBinary bool, err error)
	// Stage runs `git add` on the path.
	Stage(path string) error
	// Unstage removes the path from the index.
	Unstage(path string) error
	// StageAll stages every working-tree change (`git add -A`): modifications,
	// additions, deletions, and untracked files.
	StageAll() error
	// UnstageAll removes all paths from the index (`git reset -q HEAD`), leaving
	// the working tree untouched.
	UnstageAll() error
	// Discard reverts a working-tree change: it restores a tracked file from the
	// index (HEAD when nothing is staged) and deletes an untracked file. This is
	// destructive - the discarded working-tree changes cannot be recovered.
	Discard(fc FileChange) error
	// WorktreePatch returns the unstaged (index→working tree) patch for a path,
	// for hunk-level staging.
	WorktreePatch(path string) (FilePatch, error)
	// IndexPatch returns the staged (HEAD→index) patch for a path, for
	// hunk-level unstaging.
	IndexPatch(path string) (FilePatch, error)
	// ApplyHunk applies a single hunk to the index. reverse=false stages a
	// worktree hunk; reverse=true unstages a staged hunk.
	ApplyHunk(fp FilePatch, hunkIndex int, reverse bool) error
	// Workdir returns the repository working directory that change paths are
	// relative to, for resolving an absolute path (e.g. to open in an editor).
	Workdir() string
}

// gitSource is a Source backed by the git CLI rooted at workdir.
type gitSource struct {
	workdir string
}

// NewGitSource returns a Source rooted at workdir (the repository working dir).
func NewGitSource(workdir string) Source { return &gitSource{workdir: workdir} }

// Workdir returns the repository working directory paths are relative to.
func (g *gitSource) Workdir() string { return g.workdir }

// IsRepo reports whether workdir is inside a git work tree.
func IsRepo(workdir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = workdir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func (g *gitSource) Changes() (staged, unstaged []FileChange, err error) {
	out, err := g.run("status", "--porcelain=v1", "-z", "-uall")
	if err != nil {
		return nil, nil, err
	}
	staged, unstaged = parseStatus(out)
	return staged, unstaged, nil
}

// parseStatus parses NUL-separated `git status --porcelain=v1 -z` output. Each
// record is "XY<space>path"; for renames/copies the original path follows as a
// separate NUL-terminated token (the -z format reverses the order to new\0old).
func parseStatus(data []byte) (staged, unstaged []FileChange) {
	tokens := strings.Split(string(data), "\x00")
	for i := 0; i < len(tokens); i++ {
		entry := tokens[i]
		if len(entry) < 4 {
			continue // trailing empty token / malformed
		}
		x, y, path := entry[0], entry[1], entry[3:]

		var orig string
		if isRenameOrCopy(x) || isRenameOrCopy(y) {
			if i+1 < len(tokens) {
				orig = tokens[i+1]
				i++
			}
		}

		if x == '?' && y == '?' {
			unstaged = append(unstaged, FileChange{Path: path, Status: StatusUntracked})
			continue
		}
		if x != ' ' && x != '?' {
			staged = append(staged, FileChange{Path: path, OrigPath: orig, Status: Status(x), Staged: true})
		}
		if y != ' ' && y != '?' {
			unstaged = append(unstaged, FileChange{Path: path, OrigPath: orig, Status: Status(y), Staged: false})
		}
	}
	return staged, unstaged
}

func isRenameOrCopy(b byte) bool { return b == 'R' || b == 'C' }

func (g *gitSource) Diff(fc FileChange) (oldContent, newContent string, isBinary bool, err error) {
	if fc.Staged {
		oldContent, newContent = g.stagedContents(fc)
	} else {
		oldContent, newContent = g.unstagedContents(fc)
	}

	if notRenderable(oldContent) || notRenderable(newContent) {
		return "", "", true, nil
	}
	return oldContent, newContent, false, nil
}

// stagedContents compares HEAD → index for a staged entry.
func (g *gitSource) stagedContents(fc FileChange) (before, after string) {
	return g.show("HEAD:" + refPath(fc.OrigPath, fc.Path)), g.show(":" + fc.Path)
}

// unstagedContents compares index (HEAD fallback) → working tree for a
// working-tree entry. Untracked files have no "before".
func (g *gitSource) unstagedContents(fc FileChange) (before, after string) {
	if fc.Status != StatusUntracked {
		before = g.show(":" + refPath(fc.OrigPath, fc.Path))
		if before == "" {
			before = g.show("HEAD:" + refPath(fc.OrigPath, fc.Path))
		}
	}
	return before, g.readWorking(fc.Path)
}

func (g *gitSource) Stage(path string) error {
	_, err := g.run("add", "--", path)
	return err
}

func (g *gitSource) Unstage(path string) error {
	if _, err := g.run("restore", "--staged", "--", path); err == nil {
		return nil
	}
	// Fallback for git < 2.23 which lacks `git restore`.
	_, err := g.run("reset", "-q", "HEAD", "--", path)
	return err
}

func (g *gitSource) StageAll() error {
	_, err := g.run("add", "-A")
	return err
}

func (g *gitSource) UnstageAll() error {
	_, err := g.run("reset", "-q", "HEAD")
	return err
}

func (g *gitSource) Discard(fc FileChange) error {
	if fc.Status == StatusUntracked {
		return os.Remove(filepath.Join(g.workdir, fc.Path))
	}
	if _, err := g.run("restore", "--", fc.Path); err == nil {
		return nil
	}
	// Fallback for git < 2.23 which lacks `git restore`.
	_, err := g.run("checkout", "--", fc.Path)
	return err
}

func (g *gitSource) WorktreePatch(path string) (FilePatch, error) {
	out, err := g.run("diff", "--no-color", "--", path)
	if err != nil {
		return FilePatch{}, err
	}
	return parsePatch(string(out)), nil
}

func (g *gitSource) IndexPatch(path string) (FilePatch, error) {
	out, err := g.run("diff", "--no-color", "--cached", "--", path)
	if err != nil {
		return FilePatch{}, err
	}
	return parsePatch(string(out)), nil
}

func (g *gitSource) ApplyHunk(fp FilePatch, hunkIndex int, reverse bool) error {
	if hunkIndex < 0 || hunkIndex >= len(fp.Hunks) {
		return fmt.Errorf("hunk index %d out of range (%d hunks)", hunkIndex, len(fp.Hunks))
	}
	// --recount lets git infer line counts from the patch body, so a single
	// hunk applies cleanly even when sibling hunks are omitted.
	args := []string{"apply", "--cached", "--recount"}
	if reverse {
		args = append(args, "--reverse")
	}
	return g.runStdin(buildSingleHunkPatch(fp, hunkIndex), args...)
}

// parsePatch splits a `git diff` output into its preamble and hunks.
func parsePatch(s string) FilePatch {
	if strings.TrimSpace(s) == "" {
		return FilePatch{}
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")

	var fp FilePatch
	i := 0
	var preamble []string
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
		preamble = append(preamble, lines[i])
		i++
	}
	fp.Preamble = strings.Join(preamble, "\n")

	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}
		hunk := Hunk{Header: lines[i]}
		i++
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") {
			hunk.Lines = append(hunk.Lines, lines[i])
			i++
		}
		fp.Hunks = append(fp.Hunks, hunk)
	}
	return fp
}

// buildSingleHunkPatch assembles a minimal, applyable patch: the file preamble
// plus exactly one hunk.
func buildSingleHunkPatch(fp FilePatch, idx int) string {
	var b strings.Builder
	b.WriteString(fp.Preamble)
	b.WriteByte('\n')
	b.WriteString(fp.Hunks[idx].Header)
	b.WriteByte('\n')
	for _, l := range fp.Hunks[idx].Lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	return b.String()
}

func (g *gitSource) runStdin(stdin string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.workdir
	cmd.Stdin = strings.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// --- internals ---

// show returns the content of a git object ref (e.g. "HEAD:path", ":path").
// A missing ref (object does not exist) yields "" rather than an error.
func (g *gitSource) show(ref string) string {
	out, err := g.run("show", ref)
	if err != nil {
		return ""
	}
	return string(out)
}

func (g *gitSource) readWorking(path string) string {
	b, err := os.ReadFile(filepath.Join(g.workdir, path))
	if err != nil {
		return ""
	}
	return string(b)
}

func (g *gitSource) run(args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = g.workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// refPath prefers the rename/copy source when present, else the path itself.
func refPath(orig, path string) string {
	if orig != "" {
		return orig
	}
	return path
}

// notRenderable reports content that should be shown as "binary/large" instead
// of diffed: a NUL byte (binary) or content beyond maxRenderBytes.
func notRenderable(s string) bool {
	return len(s) > maxRenderBytes || strings.IndexByte(s, 0) >= 0
}
