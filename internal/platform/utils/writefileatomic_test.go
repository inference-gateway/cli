package utils

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	tests := []struct {
		name    string
		seed    func(t *testing.T, path string)
		perm    fs.FileMode
		wantErr bool
	}{
		{
			name: "creates a new file",
			seed: func(*testing.T, string) {},
			perm: 0o600,
		},
		{
			name: "replaces an existing file and applies perm",
			seed: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatalf("seed file: %v", err)
				}
			},
			perm: 0o644,
		},
		{
			name: "failed rename onto a non-empty directory",
			seed: func(t *testing.T, path string) {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatalf("seed directory: %v", err)
				}
				if err := os.WriteFile(filepath.Join(path, "child"), nil, 0o600); err != nil {
					t.Fatalf("seed directory child: %v", err)
				}
			},
			perm:    0o600,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "target.json")
			tt.seed(t, path)

			err := WriteFileAtomic(path, []byte("new"), tt.perm)

			if tt.wantErr != (err != nil) {
				t.Fatalf("WriteFileAtomic error = %v, wantErr %v", err, tt.wantErr)
			}
			assertNoTempFiles(t, dir)
			if tt.wantErr {
				return
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != "new" {
				t.Fatalf("content = %q, %v, want %q", got, err, "new")
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat: %v", err)
			}
			if info.Mode().Perm() != tt.perm {
				t.Fatalf("mode = %v, want %v", info.Mode().Perm(), tt.perm)
			}
		})
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}
