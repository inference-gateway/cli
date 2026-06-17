package shortcuts

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ReleaseNotesShortcut displays release notes from the CHANGELOG.md file.
// It accepts an optional version argument to show notes for a specific release.
// When no version is given, it shows the latest release notes.
type ReleaseNotesShortcut struct{}

// NewReleaseNotesShortcut creates a new release-notes shortcut.
func NewReleaseNotesShortcut() *ReleaseNotesShortcut {
	return &ReleaseNotesShortcut{}
}

func (r *ReleaseNotesShortcut) GetName() string { return "release-notes" }
func (r *ReleaseNotesShortcut) GetDescription() string {
	return "Show release notes from CHANGELOG.md for a specific version or the latest"
}
func (r *ReleaseNotesShortcut) GetUsage() string              { return "/release-notes [version]" }
func (r *ReleaseNotesShortcut) CanExecute(args []string) bool { return len(args) <= 1 }

func (r *ReleaseNotesShortcut) Execute(_ context.Context, args []string) (ShortcutResult, error) {
	changelogPath := findChangelog()
	if changelogPath == "" {
		return ShortcutResult{
			Output:  "CHANGELOG.md not found in the current or parent directories",
			Success: false,
		}, nil
	}

	content, err := os.ReadFile(changelogPath)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to read CHANGELOG.md: %v", err),
			Success: false,
		}, nil
	}

	sections := parseChangelog(string(content))
	if len(sections) == 0 {
		return ShortcutResult{
			Output:  "No release sections found in CHANGELOG.md",
			Success: false,
		}, nil
	}

	var targetVersion string
	if len(args) > 0 {
		targetVersion = strings.TrimPrefix(args[0], "v")
	}

	notes, found := findReleaseNotes(sections, targetVersion)
	if !found {
		if targetVersion != "" {
			return ShortcutResult{
				Output:  fmt.Sprintf("Release notes for version '%s' not found in CHANGELOG.md", targetVersion),
				Success: false,
			}, nil
		}
		// Fallback: show the first (latest) section
		notes = sections[0]
	}

	output := formatReleaseNotes(notes)
	return ShortcutResult{
		Output:  output,
		Success: true,
	}, nil
}

// changelogSection represents a single release section from the changelog
type changelogSection struct {
	Version string
	Date    string
	Body    string
}

// findChangelog looks for CHANGELOG.md in the current directory or parent directories
func findChangelog() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		candidate := filepath.Join(dir, "CHANGELOG.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ""
}

// parseChangelog parses the CHANGELOG.md content into sections
func parseChangelog(content string) []changelogSection {
	var sections []changelogSection
	scanner := bufio.NewScanner(strings.NewReader(content))

	var current *changelogSection
	var bodyLines []string
	inSection := false

	for scanner.Scan() {
		line := scanner.Text()

		// Match section headers like "## [0.121.1](https://...)" or "## [0.121.1]"
		if strings.HasPrefix(line, "## [") {
			// Save previous section
			if current != nil {
				current.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
				sections = append(sections, *current)
			}

			// Parse version and date from the header
			// Format: "## [version](url) (date)" or "## [version] (date)"
			headerContent := strings.TrimPrefix(line, "## ")
			headerContent = strings.TrimPrefix(headerContent, "##")

			version := extractVersion(headerContent)
			date := extractDate(headerContent)

			current = &changelogSection{
				Version: version,
				Date:    date,
			}
			bodyLines = nil
			inSection = true
			continue
		}

		if inSection && current != nil {
			bodyLines = append(bodyLines, line)
		}
	}

	// Save the last section
	if current != nil {
		current.Body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
		sections = append(sections, *current)
	}

	return sections
}

// extractVersion extracts the version from a header like "[0.121.1](url) (date)"
func extractVersion(header string) string {
	// Find the version between brackets
	start := strings.Index(header, "[")
	if start == -1 {
		return ""
	}
	end := strings.Index(header[start:], "]")
	if end == -1 {
		return ""
	}
	return header[start+1 : start+end]
}

// extractDate extracts the date from a header like "[0.121.1](url) (2026-06-11)"
func extractDate(header string) string {
	// Find the date in parentheses at the end
	// Format: "... (2026-06-11)"
	start := strings.LastIndex(header, "(")
	if start == -1 {
		return ""
	}
	end := strings.LastIndex(header, ")")
	if end == -1 || end <= start {
		return ""
	}
	date := header[start+1 : end]
	// Only return if it looks like a date
	if len(date) == 10 && date[4] == '-' && date[7] == '-' {
		return date
	}
	return ""
}

// findReleaseNotes finds the release notes for a specific version, or the latest
func findReleaseNotes(sections []changelogSection, targetVersion string) (changelogSection, bool) {
	if targetVersion == "" {
		// Return the latest (first section)
		if len(sections) > 0 {
			return sections[0], true
		}
		return changelogSection{}, false
	}

	for _, section := range sections {
		if section.Version == targetVersion {
			return section, true
		}
	}

	return changelogSection{}, false
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
