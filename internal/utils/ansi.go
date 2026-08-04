package utils

import "regexp"

// ansiEscape matches ANSI escape sequences: CSI (ESC[... - colors, cursor
// movement) and OSC (ESC]...BEL - terminal titles, hyperlinks).
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]|\x1b\][^\x07]*(?:\x07|\x1b\\)`)

// colorsDisabled mirrors the CLI's --no-colors / NO_COLOR / non-TTY decision
// (see cmd.disableOutputColors) so leaf packages can strip ANSI from
// subprocess output that would otherwise leak into the CLI's own output.
var colorsDisabled bool

// SetColorsDisabled records whether CLI output colors are disabled.
func SetColorsDisabled(disabled bool) { colorsDisabled = disabled }

// ColorsDisabled reports whether CLI output colors are disabled.
func ColorsDisabled() bool { return colorsDisabled }

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}
