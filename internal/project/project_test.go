package project

import "testing"

func TestSlugify(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"inference-gateway/cli", "inference-gateway-cli"},
		{"My Fact! v2", "my-fact-v2"},
		{"  Hello  ", "hello"},
		{"---", ""},
		{"", ""},
		{"UPPER_case.name", "upper-case-name"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}

	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	if got := Slugify(string(long)); len(got) != maxSlugLen {
		t.Errorf("Slugify long input length = %d, want %d", len(got), maxSlugLen)
	}
}

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://github.com/org/repo", "org/repo"},
		{"https://github.com/org/repo.git", "org/repo"},
		{"http://gitlab.example.com/org/repo.git", "org/repo"},
		{"git@github.com:org/repo.git", "org/repo"},
		{"git@github.com:org/repo", "org/repo"},
		{"not a url", ""},
		{"", ""},
		{"https://github.com/onlyorg", ""},
	}
	for _, tt := range tests {
		if got := ParseRemoteURL(tt.in); got != tt.want {
			t.Errorf("ParseRemoteURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
