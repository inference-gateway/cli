package project

import (
	"path/filepath"
	"testing"
)

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

func TestDetect(t *testing.T) {
	t.Run("git repo yields org/repo identity", func(t *testing.T) {
		id := detect()
		if id.Name != "inference-gateway/cli" || id.Slug != "inference-gateway-cli" {
			t.Fatalf("detect() = %+v, want inference-gateway/cli identity", id)
		}
	})

	t.Run("no remote falls back to cwd basename", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		id := detect()
		if id.Name != filepath.Base(dir) || id.Slug != Slugify(filepath.Base(dir)) {
			t.Fatalf("detect() = %+v, want cwd basename identity for %s", id, dir)
		}
	})

	t.Run("Detect caches the first result", func(t *testing.T) {
		first := Detect()
		if second := Detect(); second != first {
			t.Fatalf("Detect() changed between calls: %+v then %+v", first, second)
		}
	})
}
