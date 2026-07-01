package memory

import "path/filepath"

// memorySyncLockSuffix names the advisory lock file. It is placed as a SIBLING
// of the memory dir (never inside it) so the git working copy never tracks it.
const memorySyncLockSuffix = ".sync.lock"

// lockPath returns the sibling lock-file path for a memory dir, e.g.
// ~/.infer/memory -> ~/.infer/.memory.sync.lock.
func lockPath(dir string) string {
	return filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+memorySyncLockSuffix)
}
