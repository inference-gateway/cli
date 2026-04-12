package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
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

// handleMessage triggers the agent as a subprocess and streams responses back through the channel
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

	cm.mu.RLock()
	ch, exists := cm.channels[msg.ChannelName]
	cm.mu.RUnlock()

	if !exists {
		log.Printf("[channels] channel %s not found for response routing", msg.ChannelName)
		return
	}

	sendFn := func(content string) {
		outMsg := domain.OutboundMessage{
			ChannelName: msg.ChannelName,
			RecipientID: msg.SenderID,
			Content:     content,
			Timestamp:   time.Now(),
		}
		if err := ch.Send(ctx, outMsg); err != nil {
			log.Printf("[channels] failed to send response via %s: %v", msg.ChannelName, err)
		}
	}

	if err := cm.runAgent(ctx, sessionID, msg.Content, sendFn); err != nil {
		log.Printf("[channels] agent failed for %s/%s: %v", msg.ChannelName, msg.SenderID, err)
	}
}

// runAgent executes `infer agent --session-id <id> "<message>"` as a subprocess,
// streaming each assistant message back through the sendFn callback in real-time.
func (cm *ChannelManagerService) runAgent(ctx context.Context, sessionID, message string, sendFn func(string)) error {
	cmd := cm.execCommandFunc(ctx, os.Args[0], "agent", "--session-id", sessionID, message)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		content := formatAgentMessage(line)
		if content != "" {
			sendFn(content)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("agent process failed: %w", err)
	}

	return nil
}

// formatAgentMessage parses a JSON line from the agent's stdout and returns
// a human-readable message to send to the channel. Returns empty string for
// messages that should not be forwarded (status messages, tool results, etc.).
func formatAgentMessage(line []byte) string {
	var msg map[string]interface{}
	if err := json.Unmarshal(line, &msg); err != nil {
		return ""
	}

	// Skip status messages (type: "info", "warning", etc.)
	if _, isStatus := msg["type"]; isStatus {
		return ""
	}

	role, _ := msg["role"].(string)

	switch role {
	case "assistant":
		content, _ := msg["content"].(string)

		// If this message has tool calls, format them
		if tools, ok := msg["tools"].([]interface{}); ok && len(tools) > 0 {
			toolNames := make([]string, 0, len(tools))
			for _, t := range tools {
				if name, ok := t.(string); ok {
					toolNames = append(toolNames, name)
				}
			}
			toolMsg := fmt.Sprintf("🔧 Using tool: %s", strings.Join(toolNames, ", "))
			if content != "" {
				return content + "\n\n" + toolMsg
			}
			return toolMsg
		}

		if content != "" {
			return content
		}

	case "tool":
		return ""
	}

	return ""
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
