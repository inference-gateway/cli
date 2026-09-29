package headless

import (
	"context"
	"errors"
	"testing"
	"time"
)

// frameSink records each Write as one frame, so a test knows a command went out.
type frameSink chan string

func (s frameSink) Write(p []byte) (int, error) {
	s <- string(p)
	return len(p), nil
}

func TestStdioBrowser_Request(t *testing.T) {
	t.Run("writes one line and returns the matching result", func(t *testing.T) {
		frames := make(frameSink, 1)
		browser := newStdioBrowser(frames)
		result := make(chan string, 1)
		go func() {
			raw, err := browser.Request(t.Context(), "cmd-1", []byte(`{"type":"browser_command","id":"cmd-1"}`))
			if err != nil {
				t.Errorf("Request() err = %v", err)
			}
			result <- string(raw)
		}()
		if got := <-frames; got != "{\"type\":\"browser_command\",\"id\":\"cmd-1\"}\n" {
			t.Fatalf("stdout frame = %q, want the command as one line", got)
		}
		browser.deliver("stray", []byte(`{"id":"stray"}`))
		browser.deliver("cmd-1", []byte(`{"id":"cmd-1","title":"Example"}`))
		if got := <-result; got != `{"id":"cmd-1","title":"Example"}` {
			t.Fatalf("Request() = %s, want the result carrying cmd-1", got)
		}
	})

	t.Run("stdin EOF fails pending and later commands", func(t *testing.T) {
		frames := make(frameSink, 1)
		browser := newStdioBrowser(frames)
		failed := make(chan error, 1)
		go func() {
			_, err := browser.Request(t.Context(), "cmd-1", []byte(`{}`))
			failed <- err
		}()
		<-frames
		browser.close()
		if err := <-failed; !errors.Is(err, errBrowserHostGone) {
			t.Fatalf("pending Request() err = %v, want errBrowserHostGone", err)
		}
		if _, err := browser.Request(t.Context(), "cmd-2", []byte(`{}`)); !errors.Is(err, errBrowserHostGone) {
			t.Fatalf("Request() after EOF err = %v, want errBrowserHostGone", err)
		}
	})

	t.Run("gives up at the command deadline", func(t *testing.T) {
		browser := newStdioBrowser(make(frameSink, 1))
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		if _, err := browser.Request(ctx, "cmd-1", []byte(`{}`)); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Request() err = %v, want context.DeadlineExceeded", err)
		}
	})
}
