package shortcuts

import (
	"context"
	"errors"
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
		t.Error(`CanExecute(["0.121.0"]) = false, want true`)
	}
	if s.CanExecute([]string{"a", "b"}) {
		t.Error(`CanExecute(["a", "b"]) = true, want false`)
	}
}

func TestReleaseNotesShortcut_Execute_Success(t *testing.T) {
	s := newReleaseNotesShortcutWithFetch(func(version string) (string, error) {
		if version != "" {
			t.Errorf("expected empty version for latest, got %q", version)
		}
		return "## Release 0.121.1 (2026-06-11)\n\n### 🐛 Bug Fixes\n\n* fix a critical bug\n", nil
	})

	res, err := s.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}
	if !strings.Contains(res.Output, "Release 0.121.1") {
		t.Errorf("expected release header in output, got: %s", res.Output)
	}
	if !strings.Contains(res.Output, "fix a critical bug") {
		t.Errorf("expected body content in output, got: %s", res.Output)
	}
}

func TestReleaseNotesShortcut_Execute_SpecificVersion(t *testing.T) {
	s := newReleaseNotesShortcutWithFetch(func(version string) (string, error) {
		if version != "0.121.0" {
			t.Errorf("expected version 0.121.0, got %q", version)
		}
		return "## Release 0.121.0 (2026-06-08)\n\n### 🚀 Features\n\n* add a new feature\n", nil
	})

	res, err := s.Execute(context.Background(), []string{"0.121.0"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}
	if !strings.Contains(res.Output, "Release 0.121.0") {
		t.Errorf("expected release header in output, got: %s", res.Output)
	}
}

func TestReleaseNotesShortcut_Execute_VersionWithVPrefix(t *testing.T) {
	s := newReleaseNotesShortcutWithFetch(func(version string) (string, error) {
		if version != "0.121.0" {
			t.Errorf("expected version 0.121.0 (v prefix stripped), got %q", version)
		}
		return "## Release 0.121.0\n\ncontent\n", nil
	})

	res, err := s.Execute(context.Background(), []string{"v0.121.0"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !res.Success {
		t.Error("expected Success to be true")
	}
}

func TestReleaseNotesShortcut_Execute_VersionNotFound(t *testing.T) {
	s := newReleaseNotesShortcutWithFetch(func(version string) (string, error) {
		return "", errors.New("release notes for version '999.999.999' not found")
	})

	res, err := s.Execute(context.Background(), []string{"999.999.999"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success {
		t.Error("expected Success to be false when version is not found")
	}
	if !strings.Contains(res.Output, "not found") {
		t.Errorf("expected 'not found' message, got: %s", res.Output)
	}
}

func TestReleaseNotesShortcut_Execute_FetchError(t *testing.T) {
	s := newReleaseNotesShortcutWithFetch(func(version string) (string, error) {
		return "", errors.New("network error")
	})

	res, err := s.Execute(context.Background(), []string{"0.121.0"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if res.Success {
		t.Error("expected Success to be false on fetch error")
	}
	if !strings.Contains(res.Output, "Failed to fetch release notes") {
		t.Errorf("expected fetch error message, got: %s", res.Output)
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

func TestReleaseDataToSection(t *testing.T) {
	rd := releaseData{
		Body:        "### 🐛 Bug Fixes\n\n* fix a critical bug\n",
		TagName:     "v0.121.1",
		PublishedAt: "2026-06-11T14:24:42Z",
	}

	section := releaseDataToSection(rd)

	if section.Version != "0.121.1" {
		t.Errorf("Version = %q, want %q", section.Version, "0.121.1")
	}
	if section.Date != "2026-06-11" {
		t.Errorf("Date = %q, want %q", section.Date, "2026-06-11")
	}
	if !strings.Contains(section.Body, "fix a critical bug") {
		t.Errorf("Body should contain 'fix a critical bug', got: %s", section.Body)
	}
}

func TestReleaseDataToSection_NoVPrefix(t *testing.T) {
	rd := releaseData{
		Body:        "content",
		TagName:     "0.121.0",
		PublishedAt: "2026-06-08T00:00:00Z",
	}

	section := releaseDataToSection(rd)

	if section.Version != "0.121.0" {
		t.Errorf("Version = %q, want %q", section.Version, "0.121.0")
	}
}

func TestReleaseDataToSection_EmptyPublishedAt(t *testing.T) {
	rd := releaseData{
		Body:        "content",
		TagName:     "v0.121.0",
		PublishedAt: "",
	}

	section := releaseDataToSection(rd)

	if section.Date != "" {
		t.Errorf("expected empty date for empty PublishedAt, got %q", section.Date)
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
