package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
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

	// pendingApprovals tracks senders waiting for tool approval replies.
	// Key: senderKey ("channel-senderID"), Value: chan domain.ApprovalResponse
	pendingApprovals sync.Map

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
					logger.Error("Channel stopped with error", "channel", name, "error", err)
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
				logger.Warn("Rejected message from unauthorized user", "sender_id", msg.SenderID, "channel", msg.ChannelName)
				continue
			}

			senderKey := fmt.Sprintf("%s-%s", msg.ChannelName, msg.SenderID)
			if respChan, ok := cm.pendingApprovals.Load(senderKey); ok {
				if msg.Metadata["approval_response"] == "true" {
					respChan.(chan domain.ApprovalResponse) <- domain.ApprovalResponse{
						Type:     "approval_response",
						Approved: msg.Metadata["approved"] == "true",
					}
					continue
				}
				approved := isApprovalReply(msg.Content)
				respChan.(chan domain.ApprovalResponse) <- domain.ApprovalResponse{
					Type:     "approval_response",
					Approved: approved,
				}
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

	logger.Info("Processing message", "channel", msg.ChannelName, "sender_id", msg.SenderID, "session", sessionID)

	cm.mu.RLock()
	ch, exists := cm.channels[msg.ChannelName]
	cm.mu.RUnlock()

	if !exists {
		logger.Error("Channel not found for response routing", "channel", msg.ChannelName)
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
			logger.Error("Failed to send response", "channel", msg.ChannelName, "error", err)
		}
	}

	if err := cm.runAgent(ctx, senderKey, sessionID, msg.Content, msg.Images, sendFn, ch); err != nil {
		logger.Error("Agent failed", "channel", msg.ChannelName, "sender_id", msg.SenderID, "error", err)
	}
}

// runAgent executes `infer agent --session-id <id> "<message>"` as a subprocess,
// streaming each assistant message back through the sendFn callback in real-time.
// If images are present, they are written to session-scoped files and passed via --files flags.
// When require_approval is enabled, it also creates a stdin pipe for approval IPC.
func (cm *ChannelManagerService) runAgent(ctx context.Context, senderKey, sessionID, message string, images []domain.ImageAttachment, sendFn func(string), ch domain.Channel) error {
	args := []string{"agent", "--session-id", sessionID}

	if cm.cfg.RequireApproval {
		args = append(args, "--require-approval")
	}

	for _, img := range images {
		imgPath, err := writeSessionImage(sessionID, img)
		if err != nil {
			logger.Error("Failed to write session image", "error", err)
			continue
		}
		logger.Info("Wrote session image", "path", imgPath, "base64_bytes", len(img.Data))
		args = append(args, "--files", imgPath)
	}

	pruneSessionImages(sessionID, cm.cfg.ImageRetention)

	args = append(args, message)

	logger.Info("Running agent subprocess", "args", args)

	cmd := cm.execCommandFunc(ctx, os.Args[0], args...)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Create stdin pipe for approval responses when require_approval is enabled
	var stdinWriter io.WriteCloser
	if cm.cfg.RequireApproval {
		stdinWriter, err = cmd.StdinPipe()
		if err != nil {
			return fmt.Errorf("failed to create stdin pipe: %w", err)
		}
		defer func() {
			if err := stdinWriter.Close(); err != nil {
				logger.Error("Failed to close stdin pipe", "error", err)
			}
		}()
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Check for approval requests when require_approval is enabled
		if cm.cfg.RequireApproval && stdinWriter != nil {
			if req, ok := parseApprovalRequest(line); ok {
				if err := cm.handleApprovalRequest(ctx, senderKey, req, stdinWriter, sendFn, ch); err != nil {
					logger.Error("Approval handling failed", "error", err)
				}
				continue
			}
		}

		content := formatAgentMessage(line)
		if content != "" {
			sendFn(content)
		}
	}

	if err := cmd.Wait(); err != nil {
		if stderrBuf.Len() > 0 {
			logger.Error("Agent stderr output", "stderr", stderrBuf.String())
		}
		return fmt.Errorf("agent process failed: %w", err)
	}

	return nil
}

// handleApprovalRequest sends an approval prompt to the channel, waits for the user's reply,
// and writes the approval response to the agent's stdin.
func (cm *ChannelManagerService) handleApprovalRequest(ctx context.Context, senderKey string, req *domain.ApprovalRequest, stdinWriter io.Writer, sendFn func(string), ch domain.Channel) error {
	respChan := make(chan domain.ApprovalResponse, 1)
	cm.pendingApprovals.Store(senderKey, respChan)
	defer cm.pendingApprovals.Delete(senderKey)

	if ac, ok := ch.(domain.ApprovalChannel); ok {
		recipientID := strings.TrimPrefix(senderKey, ch.Name()+"-")
		if err := ac.SendApproval(ctx, recipientID, req); err != nil {
			logger.Error("Rich approval failed, falling back to text", "error", err)
			sendFn(formatApprovalPrompt(req))
		}
	} else {
		sendFn(formatApprovalPrompt(req))
	}

	var resp domain.ApprovalResponse
	resp.Type = "approval_response"
	resp.ToolCallID = req.ToolCallID

	select {
	case reply := <-respChan:
		resp.Approved = reply.Approved
	case <-time.After(5 * time.Minute):
		resp.Approved = false
		sendFn("⏱ Approval timed out — tool execution was automatically rejected.")
		logger.Warn("Approval timeout", "tool", req.ToolName, "sender", senderKey)
	case <-ctx.Done():
		resp.Approved = false
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal approval response: %w", err)
	}

	if _, err := stdinWriter.Write(append(respJSON, '\n')); err != nil {
		return fmt.Errorf("failed to write approval response to stdin: %w", err)
	}

	return nil
}

// parseApprovalRequest attempts to parse a JSON line as an ApprovalRequest.
func parseApprovalRequest(line []byte) (*domain.ApprovalRequest, bool) {
	var req domain.ApprovalRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, false
	}
	if req.Type != "approval_request" {
		return nil, false
	}
	return &req, true
}

// formatApprovalPrompt creates a human-readable approval prompt for the channel user.
func formatApprovalPrompt(req *domain.ApprovalRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "🔐 Tool approval required: %s\n", req.ToolName)

	var args map[string]any
	if err := json.Unmarshal([]byte(req.ToolArgs), &args); err == nil {
		if cmd, ok := args["command"].(string); ok {
			fmt.Fprintf(&sb, "Command: %s\n", cmd)
		} else if filePath, ok := args["file_path"].(string); ok {
			fmt.Fprintf(&sb, "File: %s\n", filePath)
		}
	}

	sb.WriteString("\nReply 'yes' to approve or 'no' to reject.")
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

// imageBaseDir is the root directory for session images. Tests may override this
// to use t.TempDir() so no files leak into the working tree.
var imageBaseDir = filepath.Join(config.ConfigDirName, "tmp", "channel-images")

// sessionImageDir returns the directory for storing session images under <imageBaseDir>/<sessionID>/.
func sessionImageDir(sessionID string) string {
	return filepath.Join(imageBaseDir, sessionID)
}

// writeSessionImage decodes a base64 ImageAttachment to a file in the session image directory.
func writeSessionImage(sessionID string, img domain.ImageAttachment) (string, error) {
	data, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		return "", fmt.Errorf("decoding base64: %w", err)
	}

	ext := ".jpg"
	switch img.MimeType {
	case "image/png":
		ext = ".png"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}

	dir := sessionImageDir(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating session image dir: %w", err)
	}

	name := img.Filename
	if name == "" {
		name = "channel-image"
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))

	f, err := os.CreateTemp(dir, "infer-"+name+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("creating image file: %w", err)
	}

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("writing image file: %w", err)
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing image file: %w", err)
	}

	return f.Name(), nil
}

// pruneSessionImages removes the oldest images in the session directory when
// the count exceeds the retention limit. A retention of 0 means keep all.
func pruneSessionImages(sessionID string, retention int) {
	if retention <= 0 {
		return
	}

	dir := sessionImageDir(sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var files []os.DirEntry
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "infer-") {
			files = append(files, e)
		}
	}

	if len(files) <= retention {
		return
	}

	sort.Slice(files, func(i, j int) bool {
		fi, _ := files[i].Info()
		fj, _ := files[j].Info()
		if fi == nil || fj == nil {
			return false
		}
		return fi.ModTime().Before(fj.ModTime())
	})

	toRemove := len(files) - retention
	for i := 0; i < toRemove; i++ {
		path := filepath.Join(dir, files[i].Name())
		if err := os.Remove(path); err != nil {
			logger.Warn("Failed to prune session image", "path", path, "error", err)
			continue
		}
		logger.Info("Pruned old session image", "path", path)
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
