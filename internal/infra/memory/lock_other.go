//go:build !unix

package memory

// lockDir is a no-op on platforms without flock; git's own index.lock plus the
// push-then-rebase retry remain as the concurrency safety nets.
func lockDir(string) func() { return func() {} }
