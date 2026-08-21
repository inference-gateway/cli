package components

import "strings"

// stripANSI removes ANSI escape sequences so tests can assert against the
// rendered text content without coupling to color codes.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && s[i] != 'm' && s[i] != 'K' && s[i] != 'H' && s[i] != 'J' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
