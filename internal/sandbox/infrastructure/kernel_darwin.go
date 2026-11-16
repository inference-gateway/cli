package sandboxinfra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	process "github.com/inference-gateway/cli/internal/platform/process"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	sandbox "github.com/inference-gateway/cli/internal/sandbox"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

const kernelSupported = true

// seatbeltPath is the system binary and never resolved through PATH, so a
// binary planted in the project cannot stand in for it.
const seatbeltPath = "/usr/bin/sandbox-exec"

// confinedChild runs bash under a Seatbelt profile that allows everything
// except writes outside writePaths and reads, renames and deletes of the files
// the blocking deny entries name. Seatbelt matches the resolved path, so each
// path is listed as written and with its symlinks resolved.
func confinedChild(ctx context.Context, writePaths []string, denied []sandboxdomain.Denied, command string) *Child {
	return &Child{Cmd: process.CommandContext(ctx, seatbeltPath, "-p", seatbeltProfile(writePaths, denied), "bash", "-c", command)}
}

// start is a plain start: the profile travels in the command line.
func (c *Child) start() error {
	return c.Cmd.Start()
}

func seatbeltProfile(writePaths []string, denied []sandboxdomain.Denied) string {
	var profile strings.Builder
	profile.WriteString("(version 1)(allow default)")
	if spellings := writeSpellings(writePaths); len(spellings) == 0 {
		profile.WriteString("(deny file-write*)")
	} else {
		profile.WriteString("(deny file-write* (require-not (require-any")
		for _, spelling := range spellings {
			fmt.Fprintf(&profile, " (subpath %q)", spelling)
		}
		profile.WriteString(")))")
	}
	profile.WriteString(denyRules(denied))
	return profile.String()
}

// writeSpellings lists each writable path as written and with its symlinks
// resolved, the two spellings Seatbelt matches.
func writeSpellings(writePaths []string) []string {
	var spellings []string
	for _, path := range writePaths {
		for _, spelling := range []string{path, config.RealPath(path)} {
			if !slices.Contains(spellings, spelling) {
				spellings = append(spellings, spelling)
			}
		}
	}
	return spellings
}

// denyRules refuses to read, rename or delete the file every blocking deny
// entry names, its excepts carved out. Metadata stays readable for listings.
// shortcut: directory entries and inexact globs stay with the path check since
// git must read .git/, upgrade when a secret lives in a denied directory.
func denyRules(denied []sandboxdomain.Denied) string {
	var rules strings.Builder
	for _, entry := range denied {
		rule, ok := denyRule(entry)
		if !ok {
			continue
		}
		rules.WriteString(rule)
	}
	return rules.String()
}

// denyRule turns one deny entry into a file-read-data and file-write-unlink
// deny, its excepts carved out with require-all since sibling filters would be
// or-ed. ok is false for a directory entry, and when the entry or one of its
// excepts cannot be expressed exactly.
func denyRule(entry sandboxdomain.Denied) (string, bool) {
	if isDirectoryEntry(entry.Path) {
		return "", false
	}
	main, ok := denyFilter(entry.Path)
	if !ok {
		return "", false
	}
	var excepts []string
	for _, except := range entry.Except {
		filter, ok := denyFilter(except)
		if !ok {
			return "", false
		}
		excepts = append(excepts, filter)
	}
	filter := "(require-any " + main + ")"
	if len(excepts) > 0 {
		filter = "(require-all " + filter + " (require-not (require-any " + strings.Join(excepts, " ") + ")))"
	}
	return "(deny file-read-data file-write-unlink " + filter + ")", true
}

// isDirectoryEntry reports whether a deny entry names a directory, a dir/ or
// dir/* pattern or an anchored path that is one on disk. Such an entry guards
// a tool's own tree, which the tool itself must keep reading.
func isDirectoryEntry(rulePath string) bool {
	if strings.HasSuffix(rulePath, "/") || strings.HasSuffix(rulePath, "/*") {
		return true
	}
	if !sandbox.IsAnchored(rulePath) {
		return false
	}
	info, err := os.Stat(utils.ExpandHome(rulePath))
	return err == nil && info.IsDir()
}

// denyFilter is one deny entry's path as a seatbelt path filter: an anchored
// path as its subpath spellings, a relative pattern as a regex that matches
// the same paths at any depth. ok is false for a glob shape the regex cannot
// translate exactly.
func denyFilter(rulePath string) (string, bool) {
	if sandbox.IsAnchored(rulePath) {
		return anchoredDenyFilter(rulePath), true
	}
	if strings.ContainsAny(rulePath, "?[]") {
		return "", false
	}
	switch {
	case strings.HasSuffix(rulePath, "/"), strings.HasSuffix(rulePath, "/*"):
		dir := strings.TrimSuffix(strings.TrimSuffix(rulePath, "*"), "/")
		return fmt.Sprintf(`(regex #"(^|/)%s(/|$)")`, regexEscape(dir)), true
	case strings.Contains(rulePath, "*"):
		return fmt.Sprintf(`(regex #"(^|/)%s$")`, globRegex(rulePath)), true
	default:
		return fmt.Sprintf(`(regex #"(^|/)%s$")`, regexEscape(rulePath)), true
	}
}

// anchoredDenyFilter lists an anchored deny entry as subpath spellings, both
// as written and resolved the way Seatbelt matches.
func anchoredDenyFilter(rulePath string) string {
	expanded := utils.ExpandHome(rulePath)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	var spellings []string
	for _, spelling := range []string{abs, config.RealPath(abs)} {
		if !slices.Contains(spellings, spelling) {
			spellings = append(spellings, spelling)
		}
	}
	var filters strings.Builder
	for _, spelling := range spellings {
		fmt.Fprintf(&filters, " (subpath %q)", spelling)
	}
	return strings.TrimSpace(filters.String())
}

// globRegex translates a star-only basename glob the way filepath.Match
// matches it: every star is any run of non-separator characters.
func globRegex(pattern string) string {
	parts := strings.Split(pattern, "*")
	translated := regexEscape(parts[0])
	for _, part := range parts[1:] {
		translated += "[^/]*" + regexEscape(part)
	}
	return translated
}

// regexEscape escapes the regex metacharacters in a literal.
func regexEscape(literal string) string {
	var escaped strings.Builder
	for _, r := range literal {
		if strings.ContainsRune(`\.^$+(){}|`, r) {
			escaped.WriteByte('\\')
		}
		escaped.WriteRune(r)
	}
	return escaped.String()
}
