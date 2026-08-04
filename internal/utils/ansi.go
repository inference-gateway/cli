package utils

import "github.com/charmbracelet/x/ansi"

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
	return ansi.Strip(s)
}
