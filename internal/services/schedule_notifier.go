package services

import (
	"context"
	"fmt"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// ScheduleNotifier delivers scheduled-job run output to the job's delivery
// channel. It subscribes to the scheduler's run events (Options.OnRunEvent);
// jobs without a delivery target are record-only, so their events are ignored
// here and their output lives in storage.
type ScheduleNotifier struct {
	lookup func(name string) domain.Channel
}

// NewScheduleNotifier constructs a notifier resolving channels through lookup
// (typically ChannelManagerService.GetChannel).
func NewScheduleNotifier(lookup func(name string) domain.Channel) *ScheduleNotifier {
	return &ScheduleNotifier{lookup: lookup}
}

// Notify implements the scheduler's OnRunEvent hook.
func (n *ScheduleNotifier) Notify(job domain.ScheduledJob, e domain.RunEvent) {
	if job.Channel == "" {
		return
	}
	ch := n.lookup(job.Channel)
	if ch == nil {
		logger.Warn("scheduled job delivery skipped: channel not registered", "id", job.ID, "channel", job.Channel)
		return
	}

	var content string
	switch {
	case e.Line != nil:
		content = formatAgentMessage(e.Line)
	case e.Done && e.Err != nil:
		name := job.Name
		if name == "" {
			name = job.ID
		}
		content = fmt.Sprintf("⚠️ Scheduled task %q failed: %v", name, e.Err)
	}
	if content == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out := domain.OutboundMessage{
		ChannelName: job.Channel,
		RecipientID: job.RecipientID,
		Content:     content,
		Timestamp:   time.Now(),
	}
	if err := ch.Send(ctx, out); err != nil {
		logger.Error("failed to send scheduled-job output", "id", job.ID, "channel", job.Channel, "error", err)
	}
}
