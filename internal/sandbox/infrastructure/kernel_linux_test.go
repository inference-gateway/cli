package sandboxinfra

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	llsys "github.com/landlock-lsm/go-landlock/landlock/syscall"
)

// TestStart_ConfinesWritesToTheListedPaths: a confined child writes under a
// listed directory and nowhere else, while the process that started it keeps
// writing anywhere. Skipped on a kernel without Landlock.
func TestStart_ConfinesWritesToTheListedPaths(t *testing.T) {
	if abi, err := llsys.LandlockGetABIVersion(); err != nil || abi < 1 {
		t.Skip("kernel without Landlock")
	}
	allowed := t.TempDir()
	outside := t.TempDir()

	tests := []struct {
		name    string
		probe   string
		refused bool
	}{
		{name: "under the listed directory", probe: filepath.Join(allowed, "probe")},
		{name: "outside it", probe: filepath.Join(outside, "probe"), refused: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := &Child{Cmd: exec.CommandContext(t.Context(), "sh", "-c", "touch "+tt.probe), WritePaths: []string{allowed}}
			if err := child.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			err := child.Cmd.Wait()
			_, statErr := os.Stat(tt.probe)
			if refused := err != nil && errors.Is(statErr, fs.ErrNotExist); refused != tt.refused {
				t.Fatalf("touch %s: wait err = %v, stat err = %v, want refused %v", tt.probe, err, statErr, tt.refused)
			}
		})
	}

	if err := os.WriteFile(filepath.Join(outside, "parent-probe"), []byte("x"), 0o600); err != nil {
		t.Fatalf("the parent must stay unconfined: %v", err)
	}
}
