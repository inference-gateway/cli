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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	Path     string
	OrigPath string
	Status   Status
	Staged   bool
}

// Hunk is one "@@ ... @@" section of a unified diff for a file.
type Hunk struct {
	Header string
	Lines  []string
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
	// ApplyLines applies only the selected change-lines of one hunk to the index.
	// selected holds 0-based indices into the hunk's Lines slice ('+'/'-' lines
	// only; others are ignored). reverse=false stages the selected worktree lines,
	// reverse=true unstages the selected staged lines. Unselected changes are
	// neutralized so they keep their prior staged/unstaged state.
	ApplyLines(fp FilePatch, hunkIndex int, selected map[int]bool, reverse bool) error
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

// ReadSource is the read-only subset of Source consumed by the diff viewer's PR
// tab: it lists changed files and returns their before/after content, but cannot
// stage, discard, or patch. gitSource satisfies it, and so does rangeSource.
type ReadSource interface {
	Changes() (staged, unstaged []FileChange, err error)
	Diff(fc FileChange) (oldContent, newContent string, isBinary bool, err error)
	Workdir() string
}

// rangeSource is a read-only ReadSource over the current branch's pull-request
// diff: everything between the PR base's merge-base and the working tree. It
// reuses gitSource for all git plumbing and resolves the PR base lazily via the
// gh CLI, caching the merge-base commit.
//
// ponytail: the PR tab always reflects the *checked-out* branch's PR. Reviewing
// an arbitrary un-checked-out PR would require `gh pr checkout <n>` first (we do
// not mutate the user's checkout); add that only if the need shows up.
type rangeSource struct {
	g    *gitSource
	base string // merge-base commit; resolved lazily on first use
}

// NewPRSource returns a read-only source for the current branch's PR diff,
// rooted at workdir.
func NewPRSource(workdir string) ReadSource {
	return &rangeSource{g: &gitSource{workdir: workdir}}
}

// newPRSourceWithBase builds a rangeSource against an explicit base commit,
// skipping the gh lookup. Used by tests to avoid a gh dependency.
func newPRSourceWithBase(workdir, base string) *rangeSource {
	return &rangeSource{g: &gitSource{workdir: workdir}, base: base}
}

// Workdir returns the repository working directory paths are relative to.
func (r *rangeSource) Workdir() string { return r.g.workdir }

// resolveBase resolves and caches the merge-base of the PR base branch and HEAD.
func (r *rangeSource) resolveBase() (string, error) {
	if r.base != "" {
		return r.base, nil
	}
	base, err := r.prBaseRef()
	if err != nil {
		return "", err
	}
	out, err := r.g.run("merge-base", base, "HEAD")
	if err != nil {
		return "", err
	}
	r.base = strings.TrimSpace(string(out))
	return r.base, nil
}

// prBaseRef returns the PR base branch for the current branch (via gh),
// preferring the remote-tracking ref origin/<base> when it exists locally.
func (r *rangeSource) prBaseRef() (string, error) {
	cmd := exec.Command("gh", "pr", "view", "--json", "baseRefName", "-q", ".baseRefName")
	cmd.Dir = r.g.workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("no pull request for current branch: %s", msg)
	}
	base := strings.TrimSpace(stdout.String())
	if base == "" {
		return "", fmt.Errorf("could not determine PR base branch")
	}
	if _, err := r.g.run("rev-parse", "--verify", "--quiet", "origin/"+base); err == nil {
		return "origin/" + base, nil
	}
	return base, nil
}

func (r *rangeSource) Changes() (staged, unstaged []FileChange, err error) {
	base, err := r.resolveBase()
	if err != nil {
		return nil, nil, err
	}
	out, err := r.g.run("diff", "--name-status", "-z", base)
	if err != nil {
		return nil, nil, err
	}
	return nil, parseNameStatus(out), nil
}

// parseNameStatus parses NUL-separated `git diff --name-status -z` output into a
// flat, read-only file list. Renames/copies carry the original path as a second
// token (status\0old\0new); all others are status\0path.
func parseNameStatus(data []byte) []FileChange {
	tokens := strings.Split(string(data), "\x00")
	var out []FileChange
	for i := 0; i < len(tokens); i++ {
		status := tokens[i]
		if status == "" {
			continue
		}
		code := status[0]
		if isRenameOrCopy(code) {
			if i+2 >= len(tokens) {
				break
			}
			out = append(out, FileChange{Path: tokens[i+2], OrigPath: tokens[i+1], Status: Status(code)})
			i += 2
			continue
		}
		if i+1 >= len(tokens) {
			break
		}
		out = append(out, FileChange{Path: tokens[i+1], Status: Status(code)})
		i++
	}
	return out
}

func (r *rangeSource) Diff(fc FileChange) (oldContent, newContent string, isBinary bool, err error) {
	base, err := r.resolveBase()
	if err != nil {
		return "", "", false, err
	}
	if fc.Status != StatusAdded {
		oldContent = r.g.show(base + ":" + refPath(fc.OrigPath, fc.Path))
	}
	if fc.Status != StatusDeleted {
		newContent = r.g.readWorking(fc.Path)
	}
	if notRenderable(oldContent) || notRenderable(newContent) {
		return "", "", true, nil
	}
	return oldContent, newContent, false, nil
}

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
	args := []string{"apply", "--cached", "--recount"}
	if reverse {
		args = append(args, "--reverse")
	}
	return g.runStdin(buildSingleHunkPatch(fp, hunkIndex), args...)
}

func (g *gitSource) ApplyLines(fp FilePatch, hunkIndex int, selected map[int]bool, reverse bool) error {
	if hunkIndex < 0 || hunkIndex >= len(fp.Hunks) {
		return fmt.Errorf("hunk index %d out of range (%d hunks)", hunkIndex, len(fp.Hunks))
	}
	h := fp.Hunks[hunkIndex]
	any := false
	for i, l := range h.Lines {
		if selected[i] && (firstByte(l) == '+' || firstByte(l) == '-') {
			any = true
			break
		}
	}
	if !any {
		return fmt.Errorf("no change lines selected")
	}
	args := []string{"apply", "--cached", "--recount"}
	if reverse {
		args = append(args, "--reverse")
	}
	return g.runStdin(buildLineSelectionPatch(fp, hunkIndex, selected, reverse), args...)
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

// firstByte returns the diff prefix of a patch line (' ' for an empty line).
func firstByte(s string) byte {
	if s == "" {
		return ' '
	}
	return s[0]
}

// buildLineSelectionPatch assembles a single-hunk patch that contains only the
// selected change-lines. Unselected changes are neutralized so they keep their
// current state: when staging ('+'/'-' against the index), an unselected '+' is
// dropped and an unselected '-' becomes context; when unstaging (reverse), the
// roles flip. Combined with `git apply --recount`, the result applies cleanly.
func buildLineSelectionPatch(fp FilePatch, idx int, selected map[int]bool, reverse bool) string {
	h := fp.Hunks[idx]
	var b strings.Builder
	b.WriteString(fp.Preamble)
	b.WriteByte('\n')
	b.WriteString(h.Header)
	b.WriteByte('\n')
	for i, l := range h.Lines {
		switch firstByte(l) {
		case '+':
			switch {
			case selected[i]:
				b.WriteString(l)
			case reverse:
				b.WriteByte(' ')
				b.WriteString(l[1:])
			default:
				continue
			}
		case '-':
			switch {
			case selected[i]:
				b.WriteString(l)
			case !reverse:
				b.WriteByte(' ')
				b.WriteString(l[1:])
			default:
				continue
			}
		default:
			b.WriteString(l)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// SplitFilePatchHunk returns a copy of fp with hunk idx replaced by the smallest
// independent hunks it can be broken into (see splitHunk). An out-of-range idx
// returns fp unchanged.
func SplitFilePatchHunk(fp FilePatch, idx int) FilePatch {
	if idx < 0 || idx >= len(fp.Hunks) {
		return fp
	}
	pieces := splitHunk(fp.Hunks[idx])
	if len(pieces) <= 1 {
		return fp
	}
	out := FilePatch{Preamble: fp.Preamble}
	out.Hunks = append(out.Hunks, fp.Hunks[:idx]...)
	out.Hunks = append(out.Hunks, pieces...)
	out.Hunks = append(out.Hunks, fp.Hunks[idx+1:]...)
	return out
}

// splitHunk breaks one hunk into the smallest independent hunks: one per run of
// consecutive change ('+'/'-') lines, each carrying the context around it.
// Context shared between two runs is duplicated into both pieces, which is safe
// because pieces are applied one at a time. Each piece gets a correct
// "@@ -a,b +c,d @@" header. A hunk with zero or one change run is returned as-is.
func splitHunk(h Hunk) []Hunk {
	oldStart, newStart, section := parseHunkHeader(h.Header)

	oldAt := make([]int, len(h.Lines))
	newAt := make([]int, len(h.Lines))
	o, n := oldStart, newStart
	for i, l := range h.Lines {
		oldAt[i], newAt[i] = o, n
		switch firstByte(l) {
		case '+':
			n++
		case '-':
			o++
		case '\\':
		default:
			o++
			n++
		}
	}

	isChange := func(i int) bool {
		b := firstByte(h.Lines[i])
		return b == '+' || b == '-'
	}
	var runs [][2]int
	for i := 0; i < len(h.Lines); {
		if !isChange(i) {
			i++
			continue
		}
		j := i
		for j+1 < len(h.Lines) && (isChange(j+1) || firstByte(h.Lines[j+1]) == '\\') {
			j++
		}
		runs = append(runs, [2]int{i, j})
		i = j + 1
	}
	if len(runs) <= 1 {
		return []Hunk{h}
	}

	pieces := make([]Hunk, 0, len(runs))
	for r := range runs {
		lo := 0
		if r > 0 {
			lo = runs[r-1][1] + 1
		}
		hi := len(h.Lines) - 1
		if r < len(runs)-1 {
			hi = runs[r+1][0] - 1
		}
		sub := append([]string(nil), h.Lines[lo:hi+1]...)
		oldLen, newLen := 0, 0
		for _, l := range sub {
			switch firstByte(l) {
			case '+':
				newLen++
			case '-':
				oldLen++
			case '\\':
			default:
				oldLen++
				newLen++
			}
		}
		header := fmt.Sprintf("@@ -%s +%s @@%s",
			fmtHunkRange(oldAt[lo], oldLen), fmtHunkRange(newAt[lo], newLen), section)
		pieces = append(pieces, Hunk{Header: header, Lines: sub})
	}
	return pieces
}

// fmtHunkRange renders the "start,len" side of a hunk header, collapsing a
// length of 1 to just "start" as git does.
func fmtHunkRange(start, length int) string {
	if length == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(length)
}

// parseHunkHeader extracts the old/new start lines and the trailing section text
// (everything after the closing "@@", including its leading space) from a
// "@@ -a,b +c,d @@ section" header. Missing pieces yield zero values.
func parseHunkHeader(h string) (oldStart, newStart int, section string) {
	if !strings.HasPrefix(h, "@@ ") {
		return 0, 0, ""
	}
	spec, section, found := strings.Cut(h[len("@@ "):], " @@")
	if !found {
		return 0, 0, ""
	}
	fields := strings.Fields(spec)
	if len(fields) < 2 {
		return 0, 0, ""
	}
	return parseStartNum(fields[0]), parseStartNum(fields[1]), section
}

// parseStartNum reads the start line from a hunk range token like "-12,4" or
// "+12" (sign stripped, length after the comma ignored).
func parseStartNum(s string) int {
	s = strings.TrimLeft(s, "+-")
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, _ := strconv.Atoi(s)
	return n
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
	return RunGit(context.Background(), g.workdir, args...)
}

// RunGit runs a git command in workdir (process cwd when empty) and returns
// its stdout. The context bounds the command's lifetime; stderr is folded
// into the returned error.
func RunGit(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
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
