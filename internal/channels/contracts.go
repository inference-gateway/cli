// External messaging channel contracts (Telegram, WhatsApp, ...).

package channels

import (
	"context"
	"time"

	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
)

// Channel represents a pluggable messaging transport (WhatsApp, Telegram, etc.)
type Channel interface {
	// Name returns the channel identifier (e.g., "whatsapp", "telegram")
	Name() string
	// Start begins listening for inbound messages. Blocks until ctx is cancelled.
	Start(ctx context.Context, inbox chan<- InboundMessage) error
	// Send delivers an outbound message through this channel
	Send(ctx context.Context, msg OutboundMessage) error
	// Stop gracefully shuts down the channel
	Stop() error
}

// ApprovalChannel is an optional interface that channels can implement to provide
// rich approval UIs (e.g., inline keyboard buttons) instead of text-based prompts.
type ApprovalChannel interface {
	SendApproval(ctx context.Context, recipientID string, req *ipc.ApprovalRequest) error
}

// HistoryCleaner is an optional interface that channels can implement to
// delete the chat's message history on the remote platform (used by /new).
type HistoryCleaner interface {
	ClearHistory(ctx context.Context, recipientID string) error
}

// MessageButton is a tappable button attached to an outbound message. Data is
// delivered back as inbound message content when tapped (channels that have no
// button UI ignore buttons entirely).
type MessageButton struct {
	Text string `json:"text"`
	Data string `json:"data"`
}

// ChannelCommand describes a slash command a channel may advertise natively
// (e.g., Telegram's bot command menu).
type ChannelCommand struct {
	Name        string
	Description string
}

// InboundMessage represents a message received from an external channel
type InboundMessage struct {
	ChannelName string                        `json:"channel_name"`
	SenderID    string                        `json:"sender_id"`
	Content     string                        `json:"content"`
	Images      []agentdomain.ImageAttachment `json:"images,omitempty"`
	Timestamp   time.Time                     `json:"timestamp"`
	Metadata    map[string]string             `json:"metadata,omitempty"`
}

// OutboundMessage represents a response to send back through a channel
type OutboundMessage struct {
	ChannelName string            `json:"channel_name"`
	RecipientID string            `json:"recipient_id"`
	Content     string            `json:"content"`
	Buttons     []MessageButton   `json:"buttons,omitempty"`
	Timestamp   time.Time         `json:"timestamp"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}
