package utils

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home directory unavailable in test environment: %v", err)
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "bare tilde", path: "~", want: home},
		{name: "tilde slash", path: "~/", want: home},
		{name: "tilde relative path", path: "~/src", want: filepath.Join(home, "src")},
		{name: "tilde nested path", path: "~/.infer/config.yaml", want: filepath.Join(home, ".infer", "config.yaml")},
		{name: "tilde user untouched", path: "~bob/work", want: "~bob/work"},
		{name: "bare tilde user untouched", path: "~bob", want: "~bob"},
		{name: "tilde mid path untouched", path: "/home/~x", want: "/home/~x"},
		{name: "absolute path untouched", path: "/etc/hosts", want: "/etc/hosts"},
		{name: "relative path untouched", path: "src/main.go", want: "src/main.go"},
		{name: "empty path untouched", path: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExpandHome(tt.path); got != tt.want {
				t.Errorf("ExpandHome(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestExpandHomeWithoutHomeDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.UserHomeDir reads USERPROFILE on windows")
	}
	t.Setenv("HOME", "")

	if got, want := ExpandHome("~"), "~"; got != want {
		t.Errorf("ExpandHome(~) without HOME = %q, want %q", got, want)
	}
	if got, want := ExpandHome("~/data"), "~/data"; got != want {
		t.Errorf("ExpandHome(~/data) without HOME = %q, want %q", got, want)
	}
}
