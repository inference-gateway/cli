package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// ChannelManagerService manages pluggable messaging channels and triggers
// the agent as a subprocess for each inbound message.
type ChannelManagerService struct {
	mu       sync.RWMutex
	channels map[string]domain.Channel
	inbox    chan domain.InboundMessage
	cfg      config.ChannelsConfig

	// Per-sender mutex to serialize agent invocations for the same session
	senderMutexes sync.Map // map[string]*sync.Mutex

	// semaphore limits the number of concurrent agent subprocesses
	semaphore chan struct{}

	// execCommandFunc allows overriding exec.CommandContext for testing
	execCommandFunc func(ctx context.Context, name string, args ...string) *exec.Cmd

	cancel context.CancelFunc
}

// NewChannelManagerService creates a new channel manager
func NewChannelManagerService(cfg config.ChannelsConfig) *ChannelManagerService {
	maxWorkers := cfg.MaxWorkers
	if maxWorkers <= 0 {
		maxWorkers = 5
	}

	return &ChannelManagerService{
		channels:        make(map[string]domain.Channel),
		inbox:           make(chan domain.InboundMessage, 100),
		cfg:             cfg,
		semaphore:       make(chan struct{}, maxWorkers),
		execCommandFunc: exec.CommandContext,
	}
}

// Register adds a channel to the manager
func (cm *ChannelManagerService) Register(ch domain.Channel) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.channels[ch.Name()] = ch
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

	go cm.routeInbound(ctx)

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

// routeInbound reads messages from the shared inbox and triggers the agent for each one
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

			go cm.handleMessage(ctx, msg)
		}
	}
}

// handleMessage triggers the agent as a subprocess and sends the response back through the channel
func (cm *ChannelManagerService) handleMessage(ctx context.Context, msg domain.InboundMessage) {
	select {
	case cm.semaphore <- struct{}{}:
		defer func() { <-cm.semaphore }()
	case <-ctx.Done():
		return
	}

	senderKey := fmt.Sprintf("%s-%s", msg.ChannelName, msg.SenderID)
	mu := cm.getSenderMutex(senderKey)
	mu.Lock()
	defer mu.Unlock()

	sessionID := fmt.Sprintf("channel-%s-%s", msg.ChannelName, msg.SenderID)

	log.Printf("[channels] processing message from %s/%s (session=%s)", msg.ChannelName, msg.SenderID, sessionID)

	response, err := cm.runAgent(ctx, sessionID, msg.Content)
	if err != nil {
		log.Printf("[channels] agent failed for %s/%s: %v", msg.ChannelName, msg.SenderID, err)
		return
	}

	if response == "" {
		log.Printf("[channels] agent returned empty response for %s/%s", msg.ChannelName, msg.SenderID)
		return
	}

	cm.mu.RLock()
	ch, exists := cm.channels[msg.ChannelName]
	cm.mu.RUnlock()

	if !exists {
		log.Printf("[channels] channel %s not found for response routing", msg.ChannelName)
		return
	}

	outMsg := domain.OutboundMessage{
		ChannelName: msg.ChannelName,
		RecipientID: msg.SenderID,
		Content:     response,
		Timestamp:   time.Now(),
	}

	if err := ch.Send(ctx, outMsg); err != nil {
		log.Printf("[channels] failed to send response via %s: %v", msg.ChannelName, err)
	}
}

// runAgent executes `infer agent --session-id <id> "<message>"` as a subprocess
func (cm *ChannelManagerService) runAgent(ctx context.Context, sessionID, message string) (string, error) {
	cmd := cm.execCommandFunc(ctx, os.Args[0], "agent", "--session-id", sessionID, message)
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("agent process failed: %w (stderr: %s)", err, stderr.String())
	}

	return parseAgentOutput(stdout.Bytes())
}

// parseAgentOutput extracts the last assistant response from the agent's JSON stdout
func parseAgentOutput(output []byte) (string, error) {
	var lastAssistantContent string

	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		var msg map[string]interface{}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}

		if role, ok := msg["role"].(string); ok && role == "assistant" {
			if content, ok := msg["content"].(string); ok {
				lastAssistantContent = content
			}
		}
	}

	if lastAssistantContent == "" {
		return "", fmt.Errorf("no assistant response found in agent output")
	}

	return lastAssistantContent, nil
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
