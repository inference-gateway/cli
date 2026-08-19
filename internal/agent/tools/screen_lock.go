//go:build !windows

package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// The screen is a machine-wide resource: two concurrent computer-use sessions
// would fight over one mouse and keyboard. The first session to execute a
// computer-use tool takes an exclusive flock on a lock file under ~/.infer/run;
// any other process's computer-use tools fail with an instruction to abort.
// The kernel releases the lock when the process exits, so a crashed session
// can never wedge the screen.
// ponytail: single foreground session only - when background mode lands, scope
// the lock to foreground sessions instead of every computer-use tool call.
var (
	screenLockMu   sync.Mutex
	screenLockFile *os.File
)

// acquireScreenLock takes (or re-confirms) this process's exclusive claim on
// the screen. It returns an error telling the model to abort when another
// process already holds the claim.
func acquireScreenLock() error {
	screenLockMu.Lock()
	defer screenLockMu.Unlock()

	if screenLockFile != nil {
		return nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to resolve home directory for screen lock: %w", err)
	}
	dir := filepath.Join(home, ".infer", "run")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	f, err := os.OpenFile(filepath.Join(dir, "computer-use.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open screen lock file: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return fmt.Errorf("another computer-use session is already controlling the screen; abort this computer-use task and tell the user only one foreground computer-use session can run at a time")
	}

	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	screenLockFile = f
	return nil
}
