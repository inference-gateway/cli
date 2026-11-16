package sandboxinfra

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"

	llsys "github.com/landlock-lsm/go-landlock/landlock/syscall"
	unix "golang.org/x/sys/unix"

	logger "github.com/inference-gateway/cli/internal/platform/logger"
	sandboxdomain "github.com/inference-gateway/cli/internal/sandbox/domain"
)

const kernelSupported = true

// accessRead lets a path be read and run, accessFile is every right a file can
// carry, and accessV1 is the whole first Landlock ABI, which each later one
// extends.
const (
	accessRead = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR
	accessFile = unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	accessV1 = accessRead | unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_REMOVE_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_FILE | unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK | unix.LANDLOCK_ACCESS_FS_MAKE_FIFO |
		unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK | unix.LANDLOCK_ACCESS_FS_MAKE_SYM
)

var errLandlockUnavailable = errors.New("the kernel has no Landlock")

func confinedChild(ctx context.Context, writePaths []string, _ []sandboxdomain.Denied, command string) *Child {
	child := Unconfined(ctx, command)
	child.WritePaths = writePaths
	return child
}

// start forks the child from a thread that carries the Landlock ruleset. A
// ruleset binds the thread that applies it and its children inherit it, so a
// goroutine pins a fresh thread, restricts it, forks, and lets the thread die
// with it, leaving the rest of the process as it was. Streams left unset get
// the null device first, since exec would open it for writing once restricted.
func (c *Child) start() error {
	if c.WritePaths == nil {
		return c.Cmd.Start()
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = devNull.Close() }()
	if c.Cmd.Stdin == nil {
		c.Cmd.Stdin = devNull
	}
	if c.Cmd.Stdout == nil {
		c.Cmd.Stdout = devNull
	}
	if c.Cmd.Stderr == nil {
		c.Cmd.Stderr = devNull
	}
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		done <- c.startRestricted()
	}()
	return <-done
}

// startRestricted runs on the pinned thread. A kernel without Landlock runs the
// child unconfined, as the policy promises best effort there, while any other
// failure to apply the ruleset keeps the child from starting at all.
func (c *Child) startRestricted() error {
	err := restrictThread(c.WritePaths)
	switch {
	case errors.Is(err, errLandlockUnavailable):
		logger.Debug("sandbox: kernel without Landlock, bash child runs unconfined")
	case err != nil:
		return fmt.Errorf("applying the kernel sandbox (filesystem.enforce: false in sandbox.yaml runs bash without it): %w", err)
	}
	return c.Cmd.Start()
}

// restrictThread applies to the calling thread a ruleset that lets it read and
// run everything and write only under writePaths. The handled rights grow with
// the kernel's ABI, so an older kernel enforces every right it knows.
func restrictThread(writePaths []string) error {
	abi, err := llsys.LandlockGetABIVersion()
	if errors.Is(err, unix.ENOSYS) || errors.Is(err, unix.EOPNOTSUPP) || (err == nil && abi < 1) {
		return errLandlockUnavailable
	}
	if err != nil {
		return err
	}
	handled := handledAccess(abi)
	rulesetFD, err := llsys.LandlockCreateRuleset(&llsys.RulesetAttr{HandledAccessFS: handled}, 0)
	if err != nil {
		return fmt.Errorf("creating the ruleset: %w", err)
	}
	defer func() { _ = unix.Close(rulesetFD) }()
	if err := addPathRule(rulesetFD, "/", accessRead&handled); err != nil {
		return err
	}
	for _, path := range writePaths {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		access := handled
		if !info.IsDir() {
			access = accessFile & handled
		}
		if err := addPathRule(rulesetFD, path, access); err != nil {
			return err
		}
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("setting no_new_privs: %w", err)
	}
	if err := llsys.LandlockRestrictSelf(rulesetFD, 0); err != nil {
		return fmt.Errorf("restricting the thread: %w", err)
	}
	return nil
}

// handledAccess is every filesystem right the kernel's ABI knows: the first
// ABI's thirteen, refer since the second, truncate since the third and ioctl on
// devices since the fifth.
func handledAccess(abi int) uint64 {
	access := uint64(accessV1)
	if abi >= 2 {
		access |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		access |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 5 {
		access |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return access
}

func addPathRule(rulesetFD int, path string, access uint64) error {
	fd, err := unix.Open(path, unix.O_PATH|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("opening %s for its rule: %w", path, err)
	}
	defer func() { _ = unix.Close(fd) }()
	attr := llsys.PathBeneathAttr{AllowedAccess: access, ParentFd: fd}
	if err := llsys.LandlockAddPathBeneathRule(rulesetFD, &attr, 0); err != nil {
		return fmt.Errorf("adding the rule for %s: %w", path, err)
	}
	return nil
}
