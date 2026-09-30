package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	attribute "go.opentelemetry.io/otel/attribute"
	metric "go.opentelemetry.io/otel/metric"

	config "github.com/inference-gateway/cli/config"
	chn "github.com/inference-gateway/cli/internal/channels"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// ChannelManagerService manages pluggable messaging channels and drives
// each sender's thread on the daemon's session registry. No process is
// spawned per message.
type ChannelManagerService struct {
	mu       sync.RWMutex
	channels map[string]chn.Channel
	inbox    chan chn.InboundMessage
	cfg      config.ChannelsConfig

	// Per-sender mutex to serialize the frames one chat writes to its
	// thread, so two messages of a sender cannot swap on the worker's
	// stdin.
	senderMutexes sync.Map

	// router routes one chat's frames to its thread's worker. Nil when
	// the daemon hosts no session workers (its AG-UI binding and the
	// channels are both off).
	router sessionsdomain.ThreadRouter

	// projectDir is the working dir the channel threads run in.
	projectDir string

	// threadOpts apply to a channel thread's worker when it launches:
	// the remote-control system prompt the `--remote` flag selected.
	threadOpts sessionsdomain.ThreadOptions

	// threadChats holds one render adapter per chat, keyed by sender.
	threadChats sync.Map

	// pendingApprovals tracks senders waiting for tool approval replies.
	// Key: senderKey ("channel-senderID"), Value: chan ipc.ApprovalResponse
	pendingApprovals sync.Map

	cancel context.CancelFunc

	// telemetryRecorder records daemon-level operational metrics (messages
	// processed, active channels, per-message duration). Nil-safe.
	telemetryRecorder *telemetry.Recorder

	// daemon instruments (initialised in the constructor; nil when telemetry is off)
	messagesProcessed metric.Int64Counter
	messageDuration   metric.Float64Histogram
	activeChannels    metric.Int64UpDownCounter

	// slash-command support (see channel_commands.go); nil registry disables it
	shortcutRegistry *shortcuts.Registry
	convStore        storage.ConversationStorage
	groupStore       storage.SessionGroupStorage
}

// NewChannelManagerService creates a new channel manager
func NewChannelManagerService(cfg config.ChannelsConfig, tel *telemetry.Recorder) *ChannelManagerService {
	cm := &ChannelManagerService{
		channels:          make(map[string]chn.Channel),
		inbox:             make(chan chn.InboundMessage, 100),
		cfg:               cfg,
		telemetryRecorder: tel,
	}

	cm.initDaemonInstruments()
	return cm
}

// initDaemonInstruments initialises the daemon-level OTel instruments from
// the telemetry recorder's meter. Failures are logged and dropped so
// telemetry never affects daemon operation.
func (cm *ChannelManagerService) initDaemonInstruments() {
	if cm.telemetryRecorder == nil {
		return
	}
	meter := cm.telemetryRecorder.Meter()
	if meter == nil {
		return
	}

	var err error
	if cm.messagesProcessed, err = meter.Int64Counter("infer.daemon.messages_processed",
		metric.WithDescription("Number of inbound messages processed"),
		metric.WithUnit("{message}")); err != nil {
		logger.Warn("telemetry: failed to create messages_processed counter", "error", err)
	}
	if cm.messageDuration, err = meter.Float64Histogram("infer.daemon.message.duration",
		metric.WithDescription("Per-message processing duration"),
		metric.WithUnit("s")); err != nil {
		logger.Warn("telemetry: failed to create message.duration histogram", "error", err)
	}
	if cm.activeChannels, err = meter.Int64UpDownCounter("infer.daemon.active_channels",
		metric.WithDescription("Number of active channels"),
		metric.WithUnit("{channel}")); err != nil {
		logger.Warn("telemetry: failed to create active_channels updown counter", "error", err)
	}
}

// Register adds a channel to the manager
func (cm *ChannelManagerService) Register(ch chn.Channel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.channels[ch.Name()] = ch
}

// GetChannel returns a registered channel by name, or nil if not registered.
// Used by the scheduler service to deliver scheduled-job output through the
// in-process channel registry.
func (cm *ChannelManagerService) GetChannel(name string) chn.Channel {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.channels[name]
}

// Start begins all registered channels and the message routing loop
func (cm *ChannelManagerService) Start(ctx context.Context) error {
	if !cm.cfg.Enabled {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	cm.cancel = cancel

	cm.mu.RLock()
	channels := make(map[string]chn.Channel, len(cm.channels))
	for k, v := range cm.channels {
		channels[k] = v
	}
	cm.mu.RUnlock()

	for name, ch := range channels {
		go func(name string, ch chn.Channel) {
			if cm.activeChannels != nil {
				cm.activeChannels.Add(ctx, 1)
				defer cm.activeChannels.Add(ctx, -1)
			}
			if err := ch.Start(ctx, cm.inbox); err != nil {
				if ctx.Err() == nil {
					logger.Error("channel stopped with error", "channel", name, "error", err)
				}
			}
		}(name, ch)
	}

	go cm.routeInbound(ctx)

	return nil
}

// Stop gracefully shuts down all channels and detaches the chats from
// their threads.
func (cm *ChannelManagerService) Stop() error {
	if cm.cancel != nil {
		cm.cancel()
	}
	cm.DetachThreads()

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var firstErr error
	for name, ch := range cm.channels {
		if err := ch.Stop(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("channel %s: %w", name, err)
		}
	}
	return firstErr
}

// routeInbound reads messages from the shared inbox and triggers the agent for each one
func (cm *ChannelManagerService) routeInbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-cm.inbox:
			if !cm.isAllowedUser(msg.ChannelName, msg.SenderID) {
				logger.Warn("rejected message from unauthorized user", "sender_id", msg.SenderID, "channel", msg.ChannelName)
				continue
			}

			senderKey := fmt.Sprintf("%s-%s", msg.ChannelName, msg.SenderID)
			if respChan, ok := cm.pendingApprovals.Load(senderKey); ok {
				if msg.Metadata["approval_response"] == "true" {
					deliverApprovalReply(respChan, msg.Metadata["approved"] == "true")
					continue
				}
				deliverApprovalReply(respChan, isApprovalReply(msg.Content))
				continue
			}

			if name, args, ok := cm.parseChannelCommand(msg.Content); ok {
				go cm.handleCommand(ctx, msg, name, args)
				continue
			}

			go cm.handleMessage(ctx, msg)
		}
	}
}

// deliverApprovalReply forwards an approval decision to the waiting
// resolveApproval goroutine without blocking. respChan is buffered(1) and
// one-shot, so a straggler reply (double-tap Approve, or tap + type "yes") that
// arrives after the approval already resolved is dropped rather than wedging
// routeInbound — the single consumer of cm.inbox, whose block would freeze
// message processing for every sender.
func deliverApprovalReply(respChan any, approved bool) {
	ch, ok := respChan.(chan ipc.ApprovalResponse)
	if !ok {
		return
	}
	select {
	case ch <- ipc.ApprovalResponse{Type: "approval_response", Approved: approved}:
	default:
	}
}

// threadDriver returns the registry surface, the working dir and the
// thread options the channel chats drive their threads with, and reports
// whether the registry is missing.
func (cm *ChannelManagerService) threadDriver() (sessionsdomain.ThreadRouter, string, sessionsdomain.ThreadOptions, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.router, cm.projectDir, cm.threadOpts, cm.router == nil
}

// SetThreadDriver wires the session registry the channel chats drive
// their threads through. Called by the daemon after it started the
// registry, with the working dir the workers run in and the thread
// options applied when a worker launches - the remote-control prompt the
// `--remote` flag selected before.
func (cm *ChannelManagerService) SetThreadDriver(router sessionsdomain.ThreadRouter, projectDir string, opts sessionsdomain.ThreadOptions) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.router = router
	cm.projectDir = projectDir
	cm.threadOpts = opts
}

// handleMessage delivers one inbound message to its sender's thread: a
// resume_conversation frame keeps the chat following the deterministic
// session id per sender on the daemon's working dir, and the user_message
// frame carries the text with the images as attachments for the worker to
// turn into content parts. No subprocess is spawned per message.
func (cm *ChannelManagerService) handleMessage(ctx context.Context, msg chn.InboundMessage) {
	_, projectDir, threadOpts, missing := cm.threadDriver()
	if missing {
		logger.Error("no thread registry: enable the AG-UI binding (daemon.binding) for the channels to drive threads", "channel", msg.ChannelName)
		return
	}

	cm.mu.RLock()
	ch, exists := cm.channels[msg.ChannelName]
	cm.mu.RUnlock()

	if !exists {
		logger.Error("channel not found for response routing", "channel", msg.ChannelName)
		return
	}

	senderKey := fmt.Sprintf("%s-%s", msg.ChannelName, msg.SenderID)
	senderMutex := cm.getSenderMutex(senderKey)
	senderMutex.Lock()
	defer senderMutex.Unlock()

	logger.Info("routing message to the thread", "channel", msg.ChannelName, "sender_id", msg.SenderID, "session", convdomain.FormatChannelSessionID(msg.ChannelName, msg.SenderID))

	start := time.Now()
	err := cm.threadChatFor(ctx, senderKey, ch, msg.SenderID).deliverUserMessage(ctx, msg, projectDir, threadOpts)
	cm.recordMessageProcessed(ctx, msg.ChannelName, time.Since(start), err)
	if err != nil {
		logger.Error("the thread did not take the message", "channel", msg.ChannelName, "sender_id", msg.SenderID, "error", err)
	}
}

// recordMessageProcessed records the per-message daemon metrics. Nil-safe:
// no-op when telemetry is disabled.
func (cm *ChannelManagerService) recordMessageProcessed(ctx context.Context, channel string, dur time.Duration, err error) {
	outcome := "success"
	if err != nil {
		outcome = "error"
	}
	attrs := metric.WithAttributes(
		attribute.String("infer.channel.name", channel),
		attribute.String("infer.message.outcome", outcome),
	)
	if cm.messagesProcessed != nil {
		cm.messagesProcessed.Add(ctx, 1, attrs)
	}
	if cm.messageDuration != nil {
		cm.messageDuration.Record(ctx, dur.Seconds(), attrs)
	}
}

// resolveApproval sends an approval prompt to the chat's user and waits
// for the reply (with a 5-minute auto-reject), and returns the decision.
// The caller routes the returned decision back to the thread.
func (cm *ChannelManagerService) resolveApproval(ctx context.Context, senderKey string, req ipc.ApprovalRequest, sendFn func(string), ch chn.Channel) ipc.ApprovalResponse {
	respChan := make(chan ipc.ApprovalResponse, 1)
	cm.pendingApprovals.Store(senderKey, respChan)
	defer cm.pendingApprovals.Delete(senderKey)

	if ac, ok := ch.(chn.ApprovalChannel); ok {
		recipientID := strings.TrimPrefix(senderKey, ch.Name()+"-")
		if err := ac.SendApproval(ctx, recipientID, &req); err != nil {
			logger.Error("rich approval failed, falling back to text", "error", err)
			sendFn(formatApprovalPrompt(&req))
		}
	} else {
		sendFn(formatApprovalPrompt(&req))
	}

	resp := ipc.ApprovalResponse{Type: "approval_response", ToolCallID: req.ToolCallID}
	select {
	case reply := <-respChan:
		resp.Approved = reply.Approved
	case <-time.After(constants.ApprovalTimeout):
		resp.Approved = false
		sendFn("⏱ Approval timed out - tool execution was automatically rejected.")
		logger.Warn("approval timeout", "tool", req.ToolName, "sender", senderKey)
	case <-ctx.Done():
		resp.Approved = false
	}
	return resp
}

// formatApprovalPrompt creates a human-readable approval prompt for the channel user.
func formatApprovalPrompt(req *ipc.ApprovalRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Approve %s?\n", req.ToolName)

	var args map[string]any
	if err := json.Unmarshal([]byte(req.ToolArgs), &args); err == nil {
		if cmd, ok := args["command"].(string); ok {
			fmt.Fprintf(&sb, "```\n%s\n```", cmd)
		} else if filePath, ok := args["file_path"].(string); ok {
			fmt.Fprintf(&sb, "`%s`", filePath)
		}
	}

	sb.WriteString("\n\nReply 'yes' to approve or 'no' to reject.")
	return sb.String()
}

// isApprovalReply checks if a message is an approval or rejection reply.
func isApprovalReply(content string) bool {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "yes", "y", "approve", "ok":
		return true
	default:
		return false
	}
}

// getSenderMutex returns a per-sender mutex, creating one if it doesn't exist
func (cm *ChannelManagerService) getSenderMutex(key string) *sync.Mutex {
	val, _ := cm.senderMutexes.LoadOrStore(key, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// isAllowedUser checks if a sender is in the allowed users list for the given channel
func (cm *ChannelManagerService) isAllowedUser(channelName, senderID string) bool {
	var allowedUsers []string

	switch channelName {
	case "telegram":
		allowedUsers = cm.cfg.Telegram.AllowedUsers
	case "whatsapp":
		allowedUsers = cm.cfg.WhatsApp.AllowedUsers
	default:
		return false
	}

	if len(allowedUsers) == 0 {
		return false
	}

	for _, allowed := range allowedUsers {
		if allowed == senderID {
			return true
		}
	}
	return false
}
