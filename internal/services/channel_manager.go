package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ChannelManagerService manages pluggable messaging channels and routes messages
// between external channels and the agent.
type ChannelManagerService struct {
	mu       sync.RWMutex
	channels map[string]domain.Channel
	inbox    chan domain.InboundMessage
	cfg      config.ChannelsConfig

	// Dependencies for routing
	messageQueue domain.MessageQueue
	eventBridge  domain.EventBridge

	// Track channel origins for routing responses back
	pendingResponses sync.Map // requestID -> channelOrigin

	cancel context.CancelFunc
}

// channelOrigin tracks which channel and sender a message came from
type channelOrigin struct {
	ChannelName string
	SenderID    string
}

// NewChannelManagerService creates a new channel manager
func NewChannelManagerService(cfg config.ChannelsConfig, messageQueue domain.MessageQueue) *ChannelManagerService {
	return &ChannelManagerService{
		channels:     make(map[string]domain.Channel),
		inbox:        make(chan domain.InboundMessage, 100),
		cfg:          cfg,
		messageQueue: messageQueue,
	}
}

// Register adds a channel to the manager
func (cm *ChannelManagerService) Register(ch domain.Channel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.channels[ch.Name()] = ch
}

// SetEventBridge sets the event bridge for subscribing to agent responses
func (cm *ChannelManagerService) SetEventBridge(bridge domain.EventBridge) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.eventBridge = bridge
}

// Start begins all registered channels and the message routing loop
func (cm *ChannelManagerService) Start(ctx context.Context) error {
	if !cm.cfg.Enabled {
		return nil
	}

	ctx, cancel := context.WithCancel(ctx)
	cm.cancel = cancel

	cm.mu.RLock()
	channels := make(map[string]domain.Channel, len(cm.channels))
	for k, v := range cm.channels {
		channels[k] = v
	}
	cm.mu.RUnlock()

	for name, ch := range channels {
		go func(name string, ch domain.Channel) {
			if err := ch.Start(ctx, cm.inbox); err != nil {
				if ctx.Err() == nil {
					log.Printf("[channels] channel %s stopped with error: %v", name, err)
				}
			}
		}(name, ch)
	}

	// Start the inbound message router
	go cm.routeInbound(ctx)

	// Start the outbound response router (subscribes to EventBridge)
	go cm.routeOutbound(ctx)

	return nil
}

// Stop gracefully shuts down all channels
func (cm *ChannelManagerService) Stop() error {
	if cm.cancel != nil {
		cm.cancel()
	}

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

// routeInbound reads messages from the shared inbox and enqueues them for the agent
func (cm *ChannelManagerService) routeInbound(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-cm.inbox:
			if !cm.isAllowedUser(msg.ChannelName, msg.SenderID) {
				log.Printf("[channels] rejected message from unauthorized user %s on channel %s", msg.SenderID, msg.ChannelName)
				continue
			}

			// Generate a request ID for tracking this message through the pipeline
			requestID := fmt.Sprintf("ch-%s-%s-%d", msg.ChannelName, msg.SenderID, time.Now().UnixNano())

			// Track the origin so we can route the response back
			cm.pendingResponses.Store(requestID, channelOrigin{
				ChannelName: msg.ChannelName,
				SenderID:    msg.SenderID,
			})

			// Build the SDK message with proper types
			var content sdk.MessageContent
			if err := content.FromMessageContent0(msg.Content); err != nil {
				log.Printf("[channels] failed to create message content: %v", err)
				continue
			}

			// Enqueue the message for the agent to process
			cm.messageQueue.Enqueue(sdk.Message{
				Role:    sdk.User,
				Content: content,
			}, requestID)

			log.Printf("[channels] enqueued message from %s/%s (requestID=%s)", msg.ChannelName, msg.SenderID, requestID)
		}
	}
}

// routeOutbound subscribes to the EventBridge and routes agent responses back to the originating channel
func (cm *ChannelManagerService) routeOutbound(ctx context.Context) {
	cm.mu.RLock()
	bridge := cm.eventBridge
	cm.mu.RUnlock()

	if bridge == nil {
		log.Printf("[channels] no event bridge configured, outbound routing disabled")
		return
	}

	eventChan := bridge.Subscribe()
	defer bridge.Unsubscribe(eventChan)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventChan:
			if !ok {
				return
			}
			cm.handleOutboundEvent(ctx, event)
		}
	}
}

// handleOutboundEvent processes an event and routes it back to the originating channel if applicable
func (cm *ChannelManagerService) handleOutboundEvent(ctx context.Context, event domain.ChatEvent) {
	// We only care about complete messages to send back
	completeEvent, ok := event.(domain.ChatCompleteEvent)
	if !ok {
		return
	}

	requestID := completeEvent.GetRequestID()

	// Look up the channel origin for this request
	originVal, ok := cm.pendingResponses.LoadAndDelete(requestID)
	if !ok {
		// Not a channel-originated message, skip
		return
	}

	origin := originVal.(channelOrigin)

	// Find the channel and send the response
	cm.mu.RLock()
	ch, exists := cm.channels[origin.ChannelName]
	cm.mu.RUnlock()

	if !exists {
		log.Printf("[channels] channel %s not found for response routing", origin.ChannelName)
		return
	}

	outMsg := domain.OutboundMessage{
		ChannelName: origin.ChannelName,
		RecipientID: origin.SenderID,
		Content:     completeEvent.Message,
		Timestamp:   time.Now(),
	}

	if err := ch.Send(ctx, outMsg); err != nil {
		log.Printf("[channels] failed to send response via %s: %v", origin.ChannelName, err)
	}
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

	// If no allowed users are configured, reject all (secure by default)
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
