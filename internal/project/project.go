// Package project derives the identity of the project the process runs in.
// It is a leaf package so both the agent and its tools can share it without
// import cycles.
package project

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// Identity is the detected project the process is running in. The zero value
// means global scope (no project).
type Identity struct {
	Name string
	Slug string
}

// maxSlugLen caps a slug segment, mirroring the memory tool's historical cap.
const maxSlugLen = 64

var (
	detectOnce sync.Once
	detected   Identity

	slugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)
	httpsPattern     = regexp.MustCompile(`^https?://[^/]+/([^/]+/[^/]+?)(?:\.git)?$`)
	sshPattern       = regexp.MustCompile(`^git@[^:]+:([^/]+/[^/]+?)(?:\.git)?$`)
)

// Detect returns the current project, cached for the process lifetime (chat
// sessions and `infer agent` subprocesses run with a fixed cwd). Detection
// order: git remote origin -> org/repo; else the cwd basename; else global
// (the zero Identity).
func Detect() Identity {
	detectOnce.Do(func() { detected = detect() })
	return detected
}

func detect() Identity {
	if name := RemoteName(); name != "" {
		return Identity{Name: name, Slug: Slugify(name)}
	}
	if wd, err := os.Getwd(); err == nil {
		base := filepath.Base(wd)
		if slug := Slugify(base); slug != "" && base != string(filepath.Separator) {
			return Identity{Name: base, Slug: slug}
		}
	}
	return Identity{}
}

// RemoteName returns the "org/repo" name parsed from the origin remote URL,
// or "" when there is no repo/remote or the URL is unparsable.
func RemoteName() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	return ParseRemoteURL(strings.TrimSpace(string(out)))
}

// ParseRemoteURL extracts "org/repo" from an https or scp-style ssh git remote
// URL, returning "" when it does not match either form.
func ParseRemoteURL(url string) string {
	if m := httpsPattern.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	if m := sshPattern.FindStringSubmatch(url); len(m) > 1 {
		return m[1]
	}
	return ""
}

// Slugify normalizes a name into a safe, flat filename segment: lowercased,
// non-alphanumeric runs collapsed to single hyphens, trimmed, and
// length-capped. Notably Slugify("org/repo") == "org-repo".
func Slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugInvalidChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > maxSlugLen {
		s = strings.Trim(s[:maxSlugLen], "-")
	}
	return s
}
