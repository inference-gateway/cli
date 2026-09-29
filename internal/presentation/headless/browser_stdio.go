package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

var errBrowserHostGone = errors.New("stdin closed, so the host can no longer relay the browser_result")

// stdioBrowser is the serve worker's browser extension transport. It writes each
// browser_command frame as one stdout line and resolves it with the browser_result
// line carrying the same id that the host relays on stdin, so the worker reaches
// the extension through its host and binds no port.
type stdioBrowser struct {
	out io.Writer

	mu      sync.Mutex
	pending map[string]chan json.RawMessage
	closed  bool
}

func newStdioBrowser(out io.Writer) *stdioBrowser {
	return &stdioBrowser{out: out, pending: make(map[string]chan json.RawMessage)}
}

// Request is the browser driver's ExtensionRequest seam. Each frame is one Write,
// and os.File serializes concurrent Writes, so a command never interleaves with
// the run encoder's events on the shared stdout.
func (b *stdioBrowser) Request(ctx context.Context, id string, frame json.RawMessage) (json.RawMessage, error) {
	result := make(chan json.RawMessage, 1)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, errBrowserHostGone
	}
	b.pending[id] = result
	b.mu.Unlock()
	defer b.forget(id)

	if _, err := b.out.Write(append(frame, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write the browser command: %w", err)
	}
	select {
	case raw, ok := <-result:
		if !ok {
			return nil, errBrowserHostGone
		}
		return raw, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// deliver hands a browser_result line to the Request waiting for its id. A result
// nobody waits for, a timed-out command or a stray id, is dropped.
func (b *stdioBrowser) deliver(id string, raw json.RawMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if result, ok := b.pending[id]; ok {
		result <- raw
		delete(b.pending, id)
	}
}

func (b *stdioBrowser) forget(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
}

// close fails every pending Request once stdin is gone, since no browser_result
// can arrive anymore.
func (b *stdioBrowser) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for id, result := range b.pending {
		close(result)
		delete(b.pending, id)
	}
}
