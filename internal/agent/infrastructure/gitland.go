package infrastructure

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

// ErrCheckoutDiverged is a landing refused because the packed commits do not
// continue the checkout's HEAD.
var ErrCheckoutDiverged = errors.New("the session's commits do not continue the checkout's HEAD")

// CheckoutState fingerprints a checkout the way PackWorkspace captures it:
// HEAD, the uncommitted changes and the untracked files excluded lets in. It
// also reports whether the checkout has none of either.
func CheckoutState(ctx context.Context, projectDir string, excluded func(abs string) bool) (string, bool, error) {
	head, err := utils.RunGit(ctx, projectDir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("the project is not a git checkout with a commit: %w", err)
	}
	branch, _, err := headRefs(ctx, projectDir)
	if err != nil {
		return "", false, err
	}
	patch, err := utils.RunGit(ctx, projectDir, "diff", "--binary", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("diffing the checkout: %w", err)
	}
	untracked, err := untrackedFiles(ctx, projectDir, excluded)
	if err != nil {
		return "", false, err
	}
	project, err := os.OpenRoot(projectDir)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = project.Close() }()
	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "%s\x00%s\x00", strings.TrimSpace(string(head)), branch)
	_, _ = sum.Write(patch)
	for _, rel := range untracked {
		digest, err := untrackedDigest(project, rel)
		if err != nil {
			return "", false, fmt.Errorf("reading %s: %w", rel, err)
		}
		_, _ = fmt.Fprintf(sum, "\x00%s\x00%s", rel, digest)
	}
	return hex.EncodeToString(sum.Sum(nil)), len(patch) == 0 && len(untracked) == 0, nil
}

// untrackedDigest fingerprints an untracked file the way a pack carries it: a
// regular file by its content, anything else, which a pack leaves out, by its
// kind alone.
func untrackedDigest(project *os.Root, rel string) (string, error) {
	info, err := project.Lstat(rel)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return info.Mode().Type().String(), nil
	}
	data, err := project.ReadFile(rel)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

// Landing is a packed workspace applied onto an existing checkout. It stays
// undoable until Keep: the checkout's own changes wait in a stash, and the
// untracked files excluded keeps out of a pack are never touched.
type Landing struct {
	dir      string
	head     string
	branch   string
	moved    string
	movedTip string
	stash    string
	kept     []string
}

// LandWorkspace moves the checkout at dir onto a packed workspace that
// continues it: the pack's commits fast-forward its branch, then its patch and
// files apply. A failure undoes the landing before it returns.
func LandWorkspace(ctx context.Context, ws agentdomain.Workspace, dir string, excluded func(abs string) bool) (*Landing, error) {
	l, err := beginLanding(ctx, dir, excluded)
	if err != nil {
		return nil, err
	}
	if err := l.apply(ctx, ws, excluded); err != nil {
		if undoErr := l.Undo(ctx); undoErr != nil {
			return nil, errors.Join(err, undoErr)
		}
		return nil, err
	}
	return l, nil
}

// beginLanding records where the checkout is and stashes its own changes,
// leaving the untracked files excluded keeps out where they are.
func beginLanding(ctx context.Context, dir string, excluded func(abs string) bool) (*Landing, error) {
	head, err := utils.RunGit(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("the project is not a git checkout with a commit: %w", err)
	}
	l := &Landing{dir: dir, head: strings.TrimSpace(string(head))}
	if branch, err := utils.RunGit(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil {
		l.branch = strings.TrimSpace(string(branch))
	}
	untracked, err := untrackedFiles(ctx, dir, nil)
	if err != nil {
		return nil, err
	}
	for _, rel := range untracked {
		if excluded != nil && excluded(filepath.Join(dir, rel)) {
			l.kept = append(l.kept, rel)
		}
	}
	status, err := utils.RunGit(ctx, dir, append([]string{"status", "--porcelain", "--untracked-files=all", "--", "."}, l.keptPathspecs()...)...)
	if err != nil {
		return nil, fmt.Errorf("reading the checkout's status: %w", err)
	}
	if len(strings.TrimSpace(string(status))) == 0 {
		return l, nil
	}
	args := append([]string{"stash", "push", "--quiet", "--include-untracked", "-m", "infer: the checkout before a session came back", "--", "."}, l.keptPathspecs()...)
	if _, err := utils.RunGit(ctx, dir, args...); err != nil {
		return nil, fmt.Errorf("stashing the checkout's changes: %w", err)
	}
	stash, err := utils.RunGit(ctx, dir, "rev-parse", "refs/stash")
	if err != nil {
		return nil, fmt.Errorf("reading the stash: %w", err)
	}
	l.stash = strings.TrimSpace(string(stash))
	return l, nil
}

// apply fetches the pack's commits, moves the checkout onto them and lays the
// pack's uncommitted changes and files over it.
func (l *Landing) apply(ctx context.Context, ws agentdomain.Workspace, excluded func(abs string) bool) error {
	target, err := l.fetch(ctx, ws.Bundle)
	if err != nil {
		return err
	}
	if _, err := utils.RunGit(ctx, l.dir, "merge-base", "--is-ancestor", l.head, target); err != nil {
		return ErrCheckoutDiverged
	}
	if err := l.checkout(ctx, ws.Branch, target); err != nil {
		return err
	}
	if len(ws.Patch) > 0 {
		if _, err := utils.RunGitStdin(ctx, l.dir, string(ws.Patch), "apply", "--binary"); err != nil {
			return fmt.Errorf("the session's uncommitted changes do not apply: %w", err)
		}
	}
	denied := func(rel string) bool { return excluded != nil && excluded(filepath.Join(l.dir, rel)) }
	if err := untar(ws.Untracked, l.dir, denied, MaxWorkspaceBytes); err != nil {
		return fmt.Errorf("unpacking the untracked files: %w", err)
	}
	if err := untar(ws.Extra, l.dir, denied, MaxWorkspaceBytes); err != nil {
		return fmt.Errorf("unpacking the extra files: %w", err)
	}
	return nil
}

// fetch reads the bundle's HEAD into the checkout and returns its commit.
func (l *Landing) fetch(ctx context.Context, bundle []byte) (string, error) {
	file, err := os.CreateTemp("", "infer-bundle-*.bundle")
	if err != nil {
		return "", err
	}
	path := file.Name()
	defer func() { _ = os.Remove(path) }()
	if _, err := file.Write(bundle); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	if _, err := utils.RunGit(ctx, l.dir, "fetch", "--quiet", "--no-tags", path, detachedHead); err != nil {
		return "", fmt.Errorf("fetching the session's commits: %w", err)
	}
	target, err := utils.RunGit(ctx, l.dir, "rev-parse", "FETCH_HEAD")
	if err != nil {
		return "", fmt.Errorf("reading the session's commits: %w", err)
	}
	return strings.TrimSpace(string(target)), nil
}

// checkout puts the pack's branch at target, refusing to move a local branch
// that target does not continue. A pack taken on a detached HEAD lands
// detached.
func (l *Landing) checkout(ctx context.Context, branch, target string) error {
	if branch == "" || branch == target {
		if _, err := utils.RunGit(ctx, l.dir, "checkout", "--quiet", "--detach", target); err != nil {
			return fmt.Errorf("checking out the session's commit: %w", err)
		}
		return nil
	}
	if strings.HasPrefix(branch, "-") {
		return fmt.Errorf("the workspace branch %q is not a branch", branch)
	}
	if tip, err := utils.RunGit(ctx, l.dir, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		l.movedTip = strings.TrimSpace(string(tip))
		if _, err := utils.RunGit(ctx, l.dir, "merge-base", "--is-ancestor", l.movedTip, target); err != nil {
			return fmt.Errorf("the session's branch %s does not continue the local one: %w", branch, ErrCheckoutDiverged)
		}
	}
	l.moved = branch
	if _, err := utils.RunGit(ctx, l.dir, "checkout", "--quiet", "-B", branch, target); err != nil {
		return fmt.Errorf("checking out %s: %w", branch, err)
	}
	return nil
}

// Keep ends the landing, dropping the stash of the checkout's own changes,
// which the landed workspace continues.
func (l *Landing) Keep(ctx context.Context) error {
	if l.stash == "" {
		return nil
	}
	index, err := l.stashIndex(ctx)
	if err != nil || index < 0 {
		return err
	}
	_, err = utils.RunGit(ctx, l.dir, "stash", "drop", "--quiet", fmt.Sprintf("stash@{%d}", index))
	return err
}

// Undo puts the checkout back the way the landing found it: HEAD and the
// branch it moved, without the files it wrote, and with its own changes
// popped from the stash.
func (l *Landing) Undo(ctx context.Context) error {
	if _, err := utils.RunGit(ctx, l.dir, "checkout", "--quiet", "--force", "--detach", l.head); err != nil {
		return l.stranded(fmt.Errorf("restoring HEAD: %w", err))
	}
	if l.moved != "" {
		args := []string{"branch", "--force", l.moved, l.movedTip}
		if l.movedTip == "" {
			args = []string{"branch", "--delete", "--force", l.moved}
		}
		if _, err := utils.RunGit(ctx, l.dir, args...); err != nil {
			return l.stranded(fmt.Errorf("restoring branch %s: %w", l.moved, err))
		}
	}
	if l.branch != "" {
		if _, err := utils.RunGit(ctx, l.dir, "checkout", "--quiet", "--force", l.branch); err != nil {
			return l.stranded(fmt.Errorf("checking out %s: %w", l.branch, err))
		}
	}
	if err := l.removeWritten(ctx); err != nil {
		return l.stranded(err)
	}
	if l.stash == "" {
		return nil
	}
	if _, err := utils.RunGit(ctx, l.dir, "stash", "pop", "--quiet", "--index"); err != nil {
		return l.stranded(fmt.Errorf("popping the stash: %w", err))
	}
	return nil
}

// removeWritten deletes the untracked files the landing wrote: every one but
// those it kept in place, since the checkout's own went into the stash.
func (l *Landing) removeWritten(ctx context.Context) error {
	untracked, err := untrackedFiles(ctx, l.dir, nil)
	if err != nil {
		return err
	}
	project, err := os.OpenRoot(l.dir)
	if err != nil {
		return err
	}
	defer func() { _ = project.Close() }()
	for _, rel := range untracked {
		if slices.Contains(l.kept, rel) {
			continue
		}
		if err := project.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing %s: %w", rel, err)
		}
	}
	return nil
}

// stranded names the stash holding the checkout's own changes when an undo
// could not finish, so nothing is lost.
func (l *Landing) stranded(err error) error {
	if l.stash == "" {
		return err
	}
	return fmt.Errorf("%w (the checkout's own changes are in stash %s)", err, l.stash)
}

func (l *Landing) stashIndex(ctx context.Context) (int, error) {
	out, err := utils.RunGit(ctx, l.dir, "stash", "list", "--format=%H")
	if err != nil {
		return -1, err
	}
	return slices.Index(strings.Fields(string(out)), l.stash), nil
}

// keptPathspecs leaves the kept files out of a status or a stash.
func (l *Landing) keptPathspecs() []string {
	specs := make([]string, 0, len(l.kept))
	for _, rel := range l.kept {
		specs = append(specs, ":(exclude,literal)"+filepath.ToSlash(rel))
	}
	return specs
}
