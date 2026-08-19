//go:build windows

package tools

// Computer use is not supported on Windows (see registerComputerUseTools), so
// the screen lock has nothing to protect there.
func acquireScreenLock() error {
	return nil
}
