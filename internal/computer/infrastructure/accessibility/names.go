package accessibility

import (
	"strings"
	"unicode"
)

// ApplicationNamesMatch reports whether an application (or window class)
// name matches a requested name, ignoring case, spaces and punctuation.
func ApplicationNamesMatch(owner, requested string) bool {
	if strings.EqualFold(strings.TrimSpace(owner), strings.TrimSpace(requested)) {
		return true
	}
	normalizedRequested := normalizeApplicationName(requested)
	return normalizedRequested != "" && normalizeApplicationName(owner) == normalizedRequested
}

func normalizeApplicationName(name string) string {
	var normalized strings.Builder
	for _, char := range name {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(unicode.ToLower(char))
		}
	}
	return normalized.String()
}
