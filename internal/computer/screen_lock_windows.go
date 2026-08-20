//go:build windows

package computer

// Computer use is not supported on Windows (see registerComputerUseTools), so
// the screen lock has nothing to protect there.
func acquireScreenLock() error {
	return nil
}
