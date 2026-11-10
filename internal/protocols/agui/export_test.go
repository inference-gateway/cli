package agui

import (
	"io"
	"time"
)

const OutboundQueue = outboundQueue

func NewQuietProbes(next io.Writer) io.Writer { return quietProbes{next: next} }

// SetPingInterval shortens the ping interval and returns the call that restores it.
func SetPingInterval(interval time.Duration) (restore func()) {
	previous := pingInterval
	pingInterval = interval
	return func() { pingInterval = previous }
}
