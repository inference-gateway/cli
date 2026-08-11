package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// scheduleOutputFile returns the JSONL file path where scheduled-job output is written.
// The desktop app can tail or poll this file for structured results.
// Using the home directory ensures the file is always writable regardless of CWD.
func scheduleOutputFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".infer", "tmp", "schedule-output.jsonl"), nil
}

// LocalChannel delivers scheduled-job output to a local JSONL file so the
// desktop app or headless agent can consume it without an external messaging
// platform. No inbound message handling - Start blocks until context cancel.
type LocalChannel struct {
	enabled bool
}

// NewLocalChannel creates a local channel. enabled=false means Send is a no-op.
func NewLocalChannel(enabled bool) *LocalChannel {
	return &LocalChannel{enabled: enabled}
}

// Name returns "local".
func (l *LocalChannel) Name() string { return "local" }

// Start blocks until the context is cancelled. No inbound messages - this
// channel only delivers outbound scheduled-job output via Send.
func (l *LocalChannel) Start(ctx context.Context, inbox chan<- domain.InboundMessage) error {
	<-ctx.Done()
	return ctx.Err()
}

// Send appends the message as a JSON line to the schedule output file.
// Returns nil so the scheduler never treats a local delivery as failed.
func (l *LocalChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	if !l.enabled {
		return nil
	}

	line := struct {
		JobID       string `json:"job_id,omitempty"`
		RecipientID string `json:"recipient_id"`
		Content     string `json:"content"`
		Timestamp   string `json:"timestamp"`
	}{
		RecipientID: msg.RecipientID,
		Content:     msg.Content,
		Timestamp:   msg.Timestamp.Format(time.RFC3339),
	}

	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("local channel marshal: %w", err)
	}

	outPath, err := scheduleOutputFile()
	if err != nil {
		return fmt.Errorf("local channel path: %w", err)
	}

	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("local channel mkdir: %w", err)
	}

	f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("local channel open: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			logger.Warn("local channel close", "error", cerr)
		}
	}()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("local channel write: %w", err)
	}

	return nil
}

// Stop is a no-op for the local channel.
func (l *LocalChannel) Stop() error { return nil }
