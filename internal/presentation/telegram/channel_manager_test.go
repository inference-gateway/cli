package telegram

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	channelmocks "github.com/inference-gateway/cli/tests/mocks/channels"
	sessionmocks "github.com/inference-gateway/cli/tests/mocks/sessions"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	channels "github.com/inference-gateway/cli/internal/channels"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	telemetry "github.com/inference-gateway/cli/internal/platform/telemetry"
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
)

// channelFor builds a fake channel with the given name.
func channelFor(name string) *channelmocks.FakeChannel {
	ch := &channelmocks.FakeChannel{}
	ch.NameReturns(name)
	return ch
}

func TestChannelManagerService_DaemonInstruments(t *testing.T) {
	tel := telemetry.New(telemetry.Options{Enabled: true, Dir: t.TempDir(), SessionID: "test"})
	if tel == nil {
		t.Fatal("expected recorder with file sink enabled")
	}
	defer tel.Shutdown(context.Background())

	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, tel)
	if cm.messagesProcessed == nil || cm.messageDuration == nil || cm.activeChannels == nil {
		t.Fatal("expected daemon instruments to be initialised")
	}

	cm.recordMessageProcessed(context.Background(), "telegram", time.Second, nil)
	cm.recordMessageProcessed(context.Background(), "telegram", time.Second, os.ErrClosed)
}

func TestChannelManagerService_Register(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: true}
	cm := NewChannelManagerService(cfg, nil)

	ch := &channelmocks.FakeChannel{}
	ch.NameReturns("test")
	cm.Register(ch)

	cm.mu.RLock()
	_, exists := cm.channels["test"]
	cm.mu.RUnlock()

	if !exists {
		t.Fatal("expected channel to be registered")
	}
}

func TestChannelManagerService_StartDisabled(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: false}
	cm := NewChannelManagerService(cfg, nil)

	err := cm.Start(context.Background())
	if err != nil {
		t.Fatalf("expected no error when disabled, got %v", err)
	}
}

func TestChannelManagerService_StopChannels(t *testing.T) {
	cfg := config.ChannelsConfig{Enabled: true}
	cm := NewChannelManagerService(cfg, nil)

	ch := &channelmocks.FakeChannel{}
	ch.NameReturns("test")
	ch.StartStub = func(ctx context.Context, inbox chan<- channels.InboundMessage) error {
		<-ctx.Done()
		return ctx.Err()
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	err = cm.Stop()
	if err != nil {
		t.Fatalf("unexpected stop error: %v", err)
	}

	if ch.StopCallCount() != 1 {
		t.Fatal("expected channel Stop to be called once")
	}
}

func TestChannelManagerService_IsAllowedUser(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123", "456"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	tests := []struct {
		channel  string
		senderID string
		want     bool
	}{
		{"telegram", "123", true},
		{"telegram", "456", true},
		{"telegram", "789", false},
		{"unknown", "123", false},
	}

	for _, tt := range tests {
		got := cm.isAllowedUser(tt.channel, tt.senderID)
		if got != tt.want {
			t.Errorf("isAllowedUser(%q, %q) = %v, want %v", tt.channel, tt.senderID, got, tt.want)
		}
	}
}

func TestChannelManagerService_IsAllowedUser_EmptyList(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	if cm.isAllowedUser("telegram", "123") {
		t.Fatal("expected rejection with empty allowed list")
	}
}

// InboundRouting drives one inbound message to its sender's thread: the chat
// hands the registry a resume_conversation with the deterministic session id
// per sender on the daemon's working dir, then the run_agent_input with the
// text and the images as content parts. No process is spawned per message.
func TestChannelManagerService_InboundRouting(t *testing.T) {
	cm := NewChannelManagerService(config.ChannelsConfig{Enabled: true}, nil)
	router := &sessionmocks.FakeThreadRouter{}
	projectDir := t.TempDir()
	cm.SetThreadDriver(router, projectDir, sessionsdomain.ThreadOptions{SystemPrompt: "You are remote controlled."})

	cm.Register(channelFor("telegram"))

	cm.handleMessage(context.Background(), channels.InboundMessage{
		ChannelName: "telegram",
		SenderID:    "123",
		Content:     "hello agent",
		Images: []agentdomain.ImageAttachment{{
			Filename: "shot.png",
			MimeType: "image/png",
			Data:     "aGVsbG8=",
		}},
	})

	if router.HandleCallCount() != 2 {
		t.Fatalf("expected the resume and the message on the registry, got %d frames", router.HandleCallCount())
	}
	var resume, input map[string]any
	for i := range 2 {
		_, frame := router.HandleArgsForCall(i)
		var f map[string]any
		if err := json.Unmarshal(frame, &f); err != nil {
			t.Fatalf("undecodable frame %d: %v", i, err)
		}
		switch f["type"] {
		case "resume_conversation":
			resume = f
		case "run_agent_input":
			input = f
		}
	}
	if resume == nil || input == nil {
		t.Fatalf("expected a resume_conversation and a run_agent_input frame, got %v and %v", resume, input)
	}
	if resume["id"] != "channel-telegram-123" {
		t.Errorf("expected the deterministic session id per sender, got %v", resume["id"])
	}
	if resume["project_dir"] != projectDir {
		t.Errorf("expected the thread in the daemon's working dir %q, got %v", projectDir, resume["project_dir"])
	}
	if resume["system_prompt"] != "You are remote controlled." {
		t.Errorf("expected the thread options applied when the worker launches, got %v", resume["system_prompt"])
	}
	messages, _ := input["input"].(map[string]any)["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("expected the one new user message on the run input, got %v", input)
	}
	parts, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("expected the text and the image as content parts, got %v", messages[0])
	}
	text, image := parts[0].(map[string]any), parts[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "hello agent" {
		t.Errorf("expected the message text as the first part, got %v", text)
	}
	if image["type"] != "image" || image["data"] != "aGVsbG8=" || image["mimeType"] != "image/png" || image["filename"] != "shot.png" {
		t.Errorf("expected the image carried as a content part, got %v", image)
	}
}

func TestChannelManagerService_UnauthorizedUserRejected(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"allowed_user"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)
	router := &sessionmocks.FakeThreadRouter{}
	cm.SetThreadDriver(router, t.TempDir(), sessionsdomain.ThreadOptions{})

	inboxSent := make(chan struct{}, 1)
	ch := &channelmocks.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- channels.InboundMessage) error {
		inbox <- channels.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "unauthorized_user",
			Content:     "should be rejected",
			Timestamp:   time.Now(),
		}
		inboxSent <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-inboxSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbox message")
	}

	time.Sleep(100 * time.Millisecond)

	if router.HandleCallCount() != 0 {
		t.Fatal("the thread registry must not see an unauthorized sender's message")
	}
}

func TestIsApprovalReply(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"yes", true},
		{"Yes", true},
		{"YES", true},
		{"y", true},
		{"Y", true},
		{"approve", true},
		{"ok", true},
		{"no", false},
		{"No", false},
		{"n", false},
		{"reject", false},
		{"something else", false},
		{"  yes  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isApprovalReply(tt.input)
			if got != tt.want {
				t.Errorf("isApprovalReply(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatApprovalPrompt(t *testing.T) {
	req := &ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"ls -la"}`,
		ToolCallID: "call_1",
	}

	prompt := formatApprovalPrompt(req)

	if !strings.Contains(prompt, "Bash") {
		t.Error("expected prompt to contain tool name")
	}
	if !strings.Contains(prompt, "ls -la") {
		t.Error("expected prompt to contain command")
	}
	if !strings.Contains(prompt, "yes") {
		t.Error("expected prompt to contain approval instruction")
	}
}

func TestFormatApprovalPrompt_FilePath(t *testing.T) {
	req := &ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Write",
		ToolArgs:   `{"file_path":"/tmp/test.txt","content":"hello"}`,
		ToolCallID: "call_2",
	}

	prompt := formatApprovalPrompt(req)

	if !strings.Contains(prompt, "Write") {
		t.Error("expected prompt to contain tool name")
	}
	if !strings.Contains(prompt, "/tmp/test.txt") {
		t.Error("expected prompt to contain file path")
	}
}
