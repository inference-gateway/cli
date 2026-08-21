package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// RunGit runs a git command in workdir (process cwd when empty) and returns
// its stdout. The context bounds the command's lifetime; stderr is folded
// into the returned error.
func RunGit(ctx context.Context, workdir string, args ...string) ([]byte, error) {
	return runGit(ctx, workdir, nil, args...)
}

// RunGitStdin is RunGit with stdin piped to the command, for plumbing that
// reads a patch from standard input (git apply, git am).
func RunGitStdin(ctx context.Context, workdir, stdin string, args ...string) ([]byte, error) {
	return runGit(ctx, workdir, strings.NewReader(stdin), args...)
}

func runGit(ctx context.Context, workdir string, stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = workdir
	cmd.Stdin = stdin
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
