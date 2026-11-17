package utils

import (
	"os"
	"os/signal"
	"syscall"
)

// OnShutdownSignal runs fn on SIGINT, SIGTERM or SIGHUP and then exits. SIGHUP
// is what a closing terminal sends. Pair it with sync.OnceFunc and defer the
// same fn on the normal exit path, so cleanup runs exactly once.
func OnShutdownSignal(fn func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		fn()
		os.Exit(0)
	}()
}
