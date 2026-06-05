package cmd

import "testing"

func TestCanonicalToolName(t *testing.T) {
	available := []string{"Read", "Bash", "WebSearch"}

	cases := map[string]string{
		"read":      "Read",
		"READ":      "Read",
		"Read":      "Read",
		"bash":      "Bash",
		"websearch": "WebSearch",
		"WebSearch": "WebSearch",
		"unknown":   "unknown",
	}

	for in, want := range cases {
		if got := canonicalToolName(available, in); got != want {
			t.Errorf("canonicalToolName(%q) = %q, want %q", in, got, want)
		}
	}
}
