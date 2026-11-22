package domain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubagentApprovalFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approval.json")
	want := SubagentApprovalFile{Awaiting: true, Summary: "Bash rm -rf x"}

	if err := WriteSubagentApprovalFile(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, ok := ReadSubagentApprovalFile(path)
	if !ok || got != want {
		t.Fatalf("read = %+v, %v, want %+v, true", got, ok, want)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind, stat err = %v", err)
	}
}

func TestReadSubagentApprovalFileMissingOrMalformed(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.json")
	if err := os.WriteFile(malformed, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("seed malformed file: %v", err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"absent", filepath.Join(dir, "absent.json")},
		{"malformed", malformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ReadSubagentApprovalFile(tt.path)
			if ok || got != (SubagentApprovalFile{}) {
				t.Fatalf("read = %+v, %v, want zero value, false", got, ok)
			}
		})
	}
}

func TestWriteSubagentApprovalFileFailedRenameRemovesTempFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "taken")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatalf("seed directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(path, "child"), nil, 0o600); err != nil {
		t.Fatalf("seed directory child: %v", err)
	}

	if err := WriteSubagentApprovalFile(path, SubagentApprovalFile{Awaiting: true}); err == nil {
		t.Fatalf("renaming over a non-empty directory should fail")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind after a failed rename, stat err = %v", err)
	}
}
