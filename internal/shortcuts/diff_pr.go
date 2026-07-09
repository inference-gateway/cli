package shortcuts

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// DiffPRShortcut fetches and displays the diff of a GitHub pull request via
// the gh CLI. It accepts an optional PR number; without one it shows the diff
// of the PR associated with the current branch.
type DiffPRShortcut struct{}

// NewDiffPRShortcut creates a new diff-pr shortcut.
func NewDiffPRShortcut() *DiffPRShortcut { return &DiffPRShortcut{} }

func (d *DiffPRShortcut) GetName() string { return "diff-pr" }
func (d *DiffPRShortcut) GetDescription() string {
	return "View the diff of a GitHub pull request"
}
func (d *DiffPRShortcut) GetUsage() string              { return "/diff-pr [<pr-number>]" }
func (d *DiffPRShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (d *DiffPRShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	cmdArgs := []string{"pr", "diff"}
	if len(args) > 0 {
		cmdArgs = append(cmdArgs, args[0])
	}

	cmd := exec.Command("gh", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to fetch PR diff: %s", errMsg),
			Success: false,
		}, nil
	}

	output := stdout.String()
	if strings.TrimSpace(output) == "" {
		return ShortcutResult{
			Output:  "No diff available for this PR",
			Success: true,
		}, nil
	}

	return ShortcutResult{
		Output:  output,
		Success: true,
	}, nil
}
