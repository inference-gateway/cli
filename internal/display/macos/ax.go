package macos

// Pure-Go, platform-independent half of the accessibility backend (no build
// tag so it is testable everywhere; the purego bridge lives in ax_darwin.go).

import "strings"

// normalizeAXRole turns an AX role constant into a readable label:
// "AXDockItem" -> "dock item", "AXButton" -> "button".
func normalizeAXRole(role string) string {
	role = strings.TrimPrefix(role, "AX")
	var b strings.Builder
	for i, r := range role {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}
