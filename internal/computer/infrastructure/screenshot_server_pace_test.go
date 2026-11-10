package infrastructure

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	config "github.com/inference-gateway/cli/config"
)

func countingServer(t *testing.T) (*ScreenshotServer, *atomic.Int32) {
	t.Helper()
	cfg := &config.Config{}
	cfg.ComputerUse.Screenshot.CaptureInterval = 3
	server := NewScreenshotServer(cfg, nil, "session")
	captures := &atomic.Int32{}
	server.capture = func() error {
		captures.Add(1)
		return nil
	}
	return server, captures
}

func startLoop(t *testing.T, server *ScreenshotServer) {
	t.Helper()
	server.captureCtx, server.captureStop = context.WithCancel(t.Context())
	go server.startCaptureLoop()
	t.Cleanup(func() {
		server.captureStop()
		synctest.Wait()
	})
}

func expectCaptures(t *testing.T, captures *atomic.Int32, want int32, when string) {
	t.Helper()
	synctest.Wait()
	if got := captures.Load(); got != want {
		t.Fatalf("%s: %d captures, want %d", when, got, want)
	}
}

func TestCaptureLoopFollowsTheHoldsPace(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, captures := countingServer(t)
		startLoop(t, server)
		expectCaptures(t, captures, 1, "at start")

		time.Sleep(3 * time.Second)
		expectCaptures(t, captures, 2, "after the configured interval")

		server.HoldChanged(true)
		expectCaptures(t, captures, 3, "as the person takes over")
		time.Sleep(heldCaptureInterval)
		expectCaptures(t, captures, 4, "one held interval in")
		time.Sleep(heldCaptureInterval)
		expectCaptures(t, captures, 5, "two held intervals in")

		server.HoldChanged(false)
		expectCaptures(t, captures, 6, "as the person hands back")
		time.Sleep(3*time.Second - time.Millisecond)
		expectCaptures(t, captures, 6, "just short of the configured interval")
		time.Sleep(time.Millisecond)
		expectCaptures(t, captures, 7, "back on the configured interval")
	})
}

func TestAnInputIsCapturedAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, captures := countingServer(t)
		startLoop(t, server)
		expectCaptures(t, captures, 1, "at start")

		server.InputLanded()
		expectCaptures(t, captures, 2, "right after the input")
	})
}

func awaiting(server *ScreenshotServer) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		server.awaitOwedFrame()
		close(done)
	}()
	synctest.Wait()
	return done
}

func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func TestAReaderWaitsForTheFrameAnInputOwes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, _ := countingServer(t)
		if !isClosed(awaiting(server)) {
			t.Fatal("a reader must not wait when no frame is owed")
		}

		server.InputLanded()
		reader := awaiting(server)
		if isClosed(reader) {
			t.Fatal("a reader must wait for the frame the input owes")
		}
		if err := server.captureOwedFrame(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !isClosed(reader) {
			t.Fatal("the capture after the input must release the reader")
		}
	})
}

func TestAnInputDuringACaptureStaysOwed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, captures := countingServer(t)
		server.InputLanded()
		server.capture = func() error {
			if captures.Add(1) == 1 {
				server.InputLanded()
			}
			return nil
		}

		if err := server.captureOwedFrame(); err != nil {
			t.Fatal(err)
		}
		reader := awaiting(server)
		if isClosed(reader) {
			t.Fatal("a capture that started before the input must not settle it")
		}
		if err := server.captureOwedFrame(); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if !isClosed(reader) {
			t.Fatal("the next capture must settle the input")
		}
	})
}

func TestAReaderGivesUpOnAFrameThatNeverComes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, _ := countingServer(t)
		server.InputLanded()
		started := time.Now()
		server.awaitOwedFrame()
		if waited := time.Since(started); waited != freshFrameWait {
			t.Fatalf("waited %v, want %v", waited, freshFrameWait)
		}
	})
}
