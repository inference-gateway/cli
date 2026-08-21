package utils

import (
	"os"
	"os/signal"
	"syscall"
)

// OnShutdownSignal runs fn on SIGINT, SIGTERM or SIGHUP and then exits. SIGHUP
// matters as much as the other two: it is what a closing terminal sends, and it
// is the path on which cleanup was previously skipped entirely.
//
// Pair it with sync.OnceFunc and defer the same fn on the normal exit path, so
// whichever route wins runs the cleanup exactly once.
func OnShutdownSignal(fn func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		fn()
		os.Exit(0)
	}()
}
