package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// hookPollInterval paces the waits Quiesce does on the thread's state, which
// the worker's own lines advance.
const hookPollInterval = 10 * time.Millisecond

// Quiesce brings a thread to rest for a teleport: it interrupts an open run,
// waits for it to end, stops the worker so its storage is flushed, and returns
// the interrupts the run was suspended on, as the worker wrote them. The
// thread and its clients stay, so a later frame relaunches a worker.
func (r *Registry) Quiesce(ctx context.Context, key sessionsdomain.ThreadKey) (json.RawMessage, error) {
	r.mu.Lock()
	t := r.threads[key]
	if t == nil {
		r.mu.Unlock()
		return nil, nil
	}
	w, running := t.worker, t.runID != ""
	r.mu.Unlock()
	if w == nil {
		return nil, nil
	}
	if running {
		if err := w.Send([]byte(`{"type":"interrupt"}`)); err != nil {
			return nil, fmt.Errorf("interrupting the open run: %w", err)
		}
		if err := r.await(ctx, func() bool { return t.runID == "" }); err != nil {
			return nil, fmt.Errorf("waiting for the open run to end: %w", err)
		}
	}
	r.mu.Lock()
	interrupts := slices.Clone(t.interruptsRaw)
	r.mu.Unlock()
	w.Stop()
	if err := r.await(ctx, func() bool { return t.worker != w }); err != nil {
		return nil, fmt.Errorf("waiting for the worker to exit: %w", err)
	}
	return interrupts, nil
}

// await polls cond under the registry lock until it holds or ctx ends.
func (r *Registry) await(ctx context.Context, cond func() bool) error {
	ticker := time.NewTicker(hookPollInterval)
	defer ticker.Stop()
	for {
		r.mu.Lock()
		done := cond()
		r.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Deliver sends one frame to every client following the thread.
func (r *Registry) Deliver(key sessionsdomain.ThreadKey, frame []byte) {
	r.mu.Lock()
	var clients []sessionsdomain.Client
	if t := r.threads[key]; t != nil {
		clients = slices.Collect(maps.Keys(t.clients))
	}
	r.mu.Unlock()
	for _, c := range clients {
		c.Deliver(frame)
	}
}

// replyClient is the registry's own client for one panel request: it keeps
// the frames it is delivered until the request's reply arrives.
type replyClient struct {
	frames chan []byte
}

func (c *replyClient) Deliver(frame []byte) {
	select {
	case c.frames <- frame:
	default:
	}
}

// Request sends one panel frame on a worker in the frame's project dir and
// returns the reply of replyType. A RUN_ERROR in its place is the error the
// worker or the registry raised.
func (r *Registry) Request(ctx context.Context, frame []byte, replyType string) ([]byte, error) {
	c := &replyClient{frames: make(chan []byte, 16)}
	defer r.Detach(c)
	r.Handle(c, frame)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case reply := <-c.frames:
			var head struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			}
			if json.Unmarshal(reply, &head) != nil {
				continue
			}
			switch head.Type {
			case replyType:
				return reply, nil
			case "RUN_ERROR":
				return nil, errors.New(head.Message)
			}
		}
	}
}

// AgentsDone reports whether the last run of every thread this registry ran
// finished without waiting on the user. A run still open, suspended on an
// interrupt or ended by an error is not done. known is false when it ran none.
func (r *Registry) AgentsDone() (done, known bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, finished := range r.finished {
		if !finished {
			return false, true
		}
	}
	return len(r.finished) > 0, len(r.finished) > 0
}

// TeleportedAway records that a thread's session now lives in another environment.
// Its last run here, interrupted or not, no longer keeps AgentsDone false.
func (r *Registry) TeleportedAway(key sessionsdomain.ThreadKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ran := r.finished[key]; ran {
		r.finished[key] = true
	}
}

// Idle reports whether the registry has no thread and no client, and when it
// last had either.
func (r *Registry) Idle() (time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastActive, len(r.threads) == 0 && len(r.all) == 0
}
