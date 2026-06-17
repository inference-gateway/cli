package shortcuts

import (
	"context"
	"strings"
	"testing"
)

func TestReleaseNotesShortcut_Properties(t *testing.T) {
	s := NewReleaseNotesShortcut()

	if s.GetName() != "release-notes" {
		t.Errorf("GetName() = %q, want %q", s.GetName(), "release-notes")
	}
	if s.GetUsage() != "/release-notes [version]" {
		t.Errorf("GetUsage() = %q, want %q", s.GetUsage(), "/release-notes [version]")
	}
	if s.GetDescription() == "" {
		t.Error("GetDescription() is empty")
	}
}

func TestReleaseNotesShortcut_CanExecute(t *testing.T) {
	s := NewReleaseNotesShortcut()

	if !s.CanExecute(nil) {
		t.Error("CanExecute(nil) = false, want true")
	}
	if !s.CanExecute([]string{}) {
		t.Error("CanExecute([]) = false, want true")
	}
	if !s.CanExecute([]string{"0.121.0"}) {
		t.Error("CanExecute([\"0.121.0\"]) = false, want true")
	}
	if s.CanExecute([]string{"a", "b"}) {
		t.Error("CanExecute([\"a\", \"b\"]) = true, want false")
	}
}

func TestParseChangelog(t *testing.T) {
	content := `# Changelog

All notable changes to this project will be documented in this file.

## [0.121.1](https://github.com/inference-gateway/cli/compare/v0.121.0...v0.121.1) (2026-06-11)

### 🐛 Bug Fixes

* fix a critical bug

### 🧹 Maintenance

* some maintenance work

## [0.121.0](https://github.com/inference-gateway/cli/compare/v0.120.1...v0.121.0) (2026-06-08)

### 🚀 Features

* add a new feature

### ♻️ Code Refactoring

* refactor some code
`

	sections := parseChangelog(content)

	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}

	if sections[0].Version != "0.121.1" {
		t.Errorf("sections[0].Version = %q, want %q", sections[0].Version, "0.121.1")
	}
	if sections[0].Date != "2026-06-11" {
		t.Errorf("sections[0].Date = %q, want %q", sections[0].Date, "2026-06-11")
	}
	if !strings.Contains(sections[0].Body, "fix a critical bug") {
		t.Errorf("sections[0].Body should contain 'fix a critical bug', got: %s", sections[0].Body)
	}

	if sections[1].Version != "0.121.0" {
		t.Errorf("sections[1].Version = %q, want %q", sections[1].Version, "0.121.0")
	}
	if sections[1].Date != "2026-06-08" {
		t.Errorf("sections[1].Date = %q, want %q", sections[1].Date, "2026-06-08")
	}
	if !strings.Contains(sections[1].Body, "add a new feature") {
		t.Errorf("sections[1].Body should contain 'add a new feature', got: %s", sections[1].Body)
	}
}

func TestParseChangelog_Empty(t *testing.T) {
	sections := parseChangelog("")
	if len(sections) != 0 {
		t.Errorf("expected 0 sections for empty content, got %d", len(sections))
	}
}

func TestParseChangelog_NoSections(t *testing.T) {
	content := `# Just a header

Some text without any release sections.`
	sections := parseChangelog(content)
	if len(sections) != 0 {
		t.Errorf("expected 0 sections, got %d", len(sections))
	}
}

func TestFindReleaseNotes_Latest(t *testing.T) {
	sections := []changelogSection{
		{Version: "0.121.1", Date: "2026-06-11", Body: "fixes"},
		{Version: "0.121.0", Date: "2026-06-08", Body: "features"},
	}

	section, found := findReleaseNotes(sections, "")
	if !found {
		t.Fatal("expected to find latest section")
	}
	if section.Version != "0.121.1" {
		t.Errorf("expected latest version 0.121.1, got %s", section.Version)
	}
}

func TestFindReleaseNotes_SpecificVersion(t *testing.T) {
	sections := []changelogSection{
		{Version: "0.121.1", Date: "2026-06-11", Body: "fixes"},
		{Version: "0.121.0", Date: "2026-06-08", Body: "features"},
	}

	section, found := findReleaseNotes(sections, "0.121.0")
	if !found {
		t.Fatal("expected to find version 0.121.0")
	}
	if section.Version != "0.121.0" {
		t.Errorf("expected version 0.121.0, got %s", section.Version)
	}
}

func TestFindReleaseNotes_NotFound(t *testing.T) {
	sections := []changelogSection{
		{Version: "0.121.1", Body: "fixes"},
	}

	_, found := findReleaseNotes(sections, "0.99.0")
	if found {
		t.Error("expected not to find version 0.99.0")
	}
}

func TestFindReleaseNotes_EmptySections(t *testing.T) {
	_, found := findReleaseNotes(nil, "")
	if found {
		t.Error("expected not to find anything in nil sections")
	}

	_, found = findReleaseNotes([]changelogSection{}, "")
	if found {
		t.Error("expected not to find anything in empty sections")
	}
}

func TestFormatReleaseNotes(t *testing.T) {
	section := changelogSection{
		Version: "0.121.0",
		Date:    "2026-06-08",
		Body:    "### 🚀 Features\n\n* add a new feature\n",
	}

	output := formatReleaseNotes(section)

	if !strings.Contains(output, "## Release 0.121.0") {
		t.Errorf("expected release header, got: %s", output)
	}
	if !strings.Contains(output, "(2026-06-08)") {
		t.Errorf("expected date in output, got: %s", output)
	}
	if !strings.Contains(output, "add a new feature") {
		t.Errorf("expected body content, got: %s", output)
	}
}

func TestFormatReleaseNotes_NoDate(t *testing.T) {
	section := changelogSection{
		Version: "0.121.0",
		Body:    "some changes",
	}

	output := formatReleaseNotes(section)

	if !strings.Contains(output, "## Release 0.121.0") {
		t.Errorf("expected release header, got: %s", output)
	}
	if strings.Contains(output, "()") {
		t.Errorf("expected no empty parentheses for missing date, got: %s", output)
	}
}

func TestExtractVersion(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"[0.121.1](url) (2026-06-11)", "0.121.1"},
		{"[0.121.0]", "0.121.0"},
		{"no brackets here", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractVersion(tt.header)
		if got != tt.want {
			t.Errorf("extractVersion(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestExtractDate(t *testing.T) {
	tests := []struct {
		header string
		want   string
	}{
		{"[0.121.1](url) (2026-06-11)", "2026-06-11"},
		{"[0.121.0] (2026-06-08)", "2026-06-08"},
		{"[0.121.0]", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractDate(tt.header)
		if got != tt.want {
			t.Errorf("extractDate(%q) = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestReleaseNotesShortcut_Execute_VersionNotFound(t *testing.T) {
	s := NewReleaseNotesShortcut()
	res, err := s.Execute(context.Background(), []string{"999.999.999"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success {
		t.Error("expected Success to be false when version is not found")
	}
	if !strings.Contains(res.Output, "not found in CHANGELOG.md") {
		t.Errorf("expected 'not found in CHANGELOG.md' message, got: %s", res.Output)
	}
}

func TestReleaseNotesShortcut_Execute_WithVersionArg(t *testing.T) {
	s := NewReleaseNotesShortcut()
	if !s.CanExecute([]string{"0.121.0"}) {
		t.Error("CanExecute with version arg should be true")
	}
	if !s.CanExecute([]string{"v0.121.0"}) {
		t.Error("CanExecute with 'v' prefixed version arg should be true")
	}
}
