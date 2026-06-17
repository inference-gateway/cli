package shortcuts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ReleaseNotesShortcut displays release notes from GitHub Releases.
// It accepts an optional version argument to show notes for a specific release.
// When no version is given, it shows the latest release notes.
type ReleaseNotesShortcut struct {
	fetchFn func(version string) (string, error)
}

// NewReleaseNotesShortcut creates a new release-notes shortcut that fetches
// release notes from GitHub Releases using the gh CLI.
func NewReleaseNotesShortcut() *ReleaseNotesShortcut {
	return &ReleaseNotesShortcut{
		fetchFn: fetchReleaseNotesFromGH,
	}
}

// newReleaseNotesShortcutWithFetch creates a release-notes shortcut with a
// custom fetch function, used for testing.
func newReleaseNotesShortcutWithFetch(fn func(version string) (string, error)) *ReleaseNotesShortcut {
	return &ReleaseNotesShortcut{
		fetchFn: fn,
	}
}

func (r *ReleaseNotesShortcut) GetName() string { return "release-notes" }
func (r *ReleaseNotesShortcut) GetDescription() string {
	return "Show release notes from GitHub Releases for a specific version or the latest"
}
func (r *ReleaseNotesShortcut) GetUsage() string              { return "/release-notes [version]" }
func (r *ReleaseNotesShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (r *ReleaseNotesShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	var targetVersion string
	if len(args) > 0 {
		targetVersion = strings.TrimPrefix(args[0], "v")
	}

	notes, err := r.fetchFn(targetVersion)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to fetch release notes: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:  notes,
		Success: true,
	}, nil
}

// changelogSection represents a single release section from the changelog
type changelogSection struct {
	Version string
	Date    string
	Body    string
}

// releaseData represents the JSON output from gh release view --json body,tagName,publishedAt
type releaseData struct {
	Body        string `json:"body"`
	TagName     string `json:"tagName"`
	PublishedAt string `json:"publishedAt"`
}

// fetchReleaseNotesFromGH fetches release notes from GitHub Releases using the gh CLI.
func fetchReleaseNotesFromGH(version string) (string, error) {
	args := []string{"release", "view", "--json", "body,tagName,publishedAt"}
	if version != "" {
		args = append(args, "v"+version)
	}

	cmd := exec.Command("gh", args...)
	output, err := cmd.Output()
	if err != nil {
		return "", handleGHError(err, version)
	}

	var release releaseData
	if err := json.Unmarshal(output, &release); err != nil {
		return "", fmt.Errorf("failed to parse release data: %w", err)
	}

	return formatReleaseNotes(releaseDataToSection(release)), nil
}

// handleGHError processes errors from the gh CLI and returns user-friendly error messages.
func handleGHError(err error, version string) error {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		stderr := string(exitErr.Stderr)
		if strings.Contains(stderr, "not found") || strings.Contains(stderr, "release not found") {
			if version != "" {
				return fmt.Errorf("release notes for version '%s' not found", version)
			}
			return fmt.Errorf("no releases found")
		}
		return fmt.Errorf("gh command failed: %s", stderr)
	}
	if strings.Contains(err.Error(), "executable file not found") {
		return fmt.Errorf("gh CLI is not installed. Please install GitHub CLI (https://cli.github.com/) to use this shortcut")
	}
	return fmt.Errorf("failed to run gh: %w", err)
}

// releaseDataToSection converts a releaseData from the GitHub API into a changelogSection.
func releaseDataToSection(r releaseData) changelogSection {
	date := ""
	if len(r.PublishedAt) >= 10 {
		date = r.PublishedAt[:10]
	}

	version := strings.TrimPrefix(r.TagName, "v")

	return changelogSection{
		Version: version,
		Date:    date,
		Body:    strings.TrimSpace(r.Body),
	}
}

// formatReleaseNotes formats a changelog section for display
func formatReleaseNotes(section changelogSection) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## Release %s", section.Version)
	if section.Date != "" {
		fmt.Fprintf(&b, " (%s)", section.Date)
	}
	b.WriteString("\n\n")

	if section.Body != "" {
		b.WriteString(section.Body)
		b.WriteString("\n")
	}

	return b.String()
}
