package process

import (
	"bufio"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestStopKillsAProcessThatIgnoresTheRequestOnceGracePasses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows has no SIGTERM to ignore")
	}
	stubborn := exec.Command("sh", "-c", "trap '' TERM; echo ready; exec sleep 30")
	stdout, err := stubborn.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := stubborn.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatalf("waiting for the trap: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = stubborn.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = stubborn.Process.Kill()
		<-exited
	})
	const grace = 300 * time.Millisecond

	started := time.Now()
	if err := Stop(t.Context(), int32(stubborn.Process.Pid), grace); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the process still runs")
	}
	if waited := time.Since(started); waited < grace {
		t.Fatalf("killed after %v, before its grace of %v", waited, grace)
	}
}
