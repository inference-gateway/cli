package states

import (
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

// CallOrder keeps a tool batch's side effects in the order the model issued
// them while still running independent calls concurrently. Next must be called
// in batch order from one goroutine at a time; the zero value is ready to use.
type CallOrder struct {
	barrier <-chan struct{}   // done of the last non-read-only call
	since   []<-chan struct{} // done of read-only calls issued after it
}

// Next registers the next call in the batch. The executor calls wait before
// running it (before taking any concurrency slot, so a waiting call never
// holds one) and done once it finishes.
func (o *CallOrder) Next(toolName string) (wait func(), done func()) {
	d := make(chan struct{})
	deps := []<-chan struct{}{o.barrier}
	// Read-only calls run together; anything else waits for every earlier
	// call, so a Read issued after a Write still sees the written file.
	if agentdomain.ReadOnlyTools[toolName] {
		o.since = append(o.since, d)
	} else {
		deps = append(deps, o.since...)
		o.barrier, o.since = d, nil
	}
	return func() {
			for _, c := range deps {
				if c != nil {
					<-c
				}
			}
		}, func() {
			close(d)
		}
}
