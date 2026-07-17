package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	telemetry "github.com/inference-gateway/cli/internal/telemetry"
	fakesdomain "github.com/inference-gateway/cli/tests/mocks/domain"
)

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

	ch := &fakesdomain.FakeChannel{}
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

	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("test")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
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

func TestChannelManagerService_InboundRouting(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"Hello from agent!","timestamp":"2024-01-01T00:00:00Z"}`)
	}

	responseSent := make(chan domain.OutboundMessage, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "hello agent",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		responseSent <- msg
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case msg := <-responseSent:
		if msg.Content != "Hello from agent!" {
			t.Errorf("expected 'Hello from agent!', got %q", msg.Content)
		}
		if msg.RecipientID != "123" {
			t.Errorf("expected recipient '123', got %q", msg.RecipientID)
		}
		if msg.ChannelName != "telegram" {
			t.Errorf("expected channel 'telegram', got %q", msg.ChannelName)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}
}

func TestChannelManagerService_StreamingMultipleMessages(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "printf",
			`{"role":"assistant","content":"Let me check...","tools":["Read"]}\n{"role":"tool","content":"file contents","tool_call_id":"123"}\n{"role":"assistant","content":"Here are the results."}`)
	}

	var messages []domain.OutboundMessage
	allSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "read my files",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		messages = append(messages, msg)
		if len(messages) == 3 {
			allSent <- struct{}{}
		}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-allSent:
		if len(messages) != 3 {
			t.Fatalf("expected 3 messages, got %d", len(messages))
		}
		if messages[0].Content != "Let me check...\n\nRead" {
			t.Errorf("expected tool message, got %q", messages[0].Content)
		}
		if messages[1].Content != "```\nfile contents\n```" {
			t.Errorf("expected tool result, got %q", messages[1].Content)
		}
		if messages[2].Content != "Here are the results." {
			t.Errorf("expected final answer, got %q", messages[2].Content)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for messages, got %d", len(messages))
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

	agentCalled := false
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		agentCalled = true
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"should not happen"}`)
	}

	inboxSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
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

	err := cm.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-inboxSent:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbox message")
	}

	time.Sleep(100 * time.Millisecond)

	if agentCalled {
		t.Fatal("agent should not have been called for unauthorized user")
	}
}

func TestFormatAgentMessage(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "assistant text message",
			line: `{"role":"assistant","content":"Hello!"}`,
			want: "Hello!",
		},
		{
			name: "assistant with tool calls",
			line: `{"role":"assistant","content":"Let me check...","tools":["Read","Grep"]}`,
			want: "Let me check...\n\nRead\nGrep",
		},
		{
			name: "assistant with tool calls no content",
			line: `{"role":"assistant","content":"","tools":["Write"]}`,
			want: "Write",
		},
		{
			name: "assistant tool call with args goes monospace",
			line: `{"role":"assistant","content":"","tools":["Bash(command=ls -la)"]}`,
			want: "Bash: `command=ls -la`",
		},
		{
			name: "tool result forwarded in code block",
			line: `{"role":"tool","content":"file contents","tool_call_id":"123"}`,
			want: "```\nfile contents\n```",
		},
		{
			name: "empty tool result is skipped",
			line: `{"role":"tool","content":"  ","tool_call_id":"123"}`,
			want: "",
		},
		{
			name: "status message is skipped",
			line: `{"type":"info","message":"Starting session"}`,
			want: "",
		},
		{
			name: "user message is skipped",
			line: `{"role":"user","content":"hello"}`,
			want: "",
		},
		{
			name: "malformed JSON is skipped",
			line: `not json`,
			want: "",
		},
		{
			name: "empty assistant content is skipped",
			line: `{"role":"assistant","content":""}`,
			want: "",
		},
		{
			name: "agent error with message",
			line: `{"type":"agent_error","message":"context length exceeded"}`,
			want: "Error: context length exceeded",
		},
		{
			name: "agent error with empty message falls back",
			line: `{"type":"agent_error","message":""}`,
			want: "Error: agent failed",
		},
		{
			name: "agent error with no message field falls back",
			line: `{"type":"agent_error"}`,
			want: "Error: agent failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatAgentMessage([]byte(tt.line))
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAgentError(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"agent error", `{"type":"agent_error","message":"boom"}`, true},
		{"agent error no message", `{"type":"agent_error"}`, true},
		{"info message", `{"type":"info","message":"x"}`, false},
		{"approval request", `{"type":"approval_request","tool_name":"Bash"}`, false},
		{"assistant message", `{"role":"assistant","content":"hi"}`, false},
		{"malformed json", `not json`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseAgentError([]byte(tt.line)); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTailStderr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"empty falls back", "", 100, "unknown error"},
		{"whitespace only falls back", "   \n\t  ", 100, "unknown error"},
		{"short string returned as-is", "boom", 100, "boom"},
		{"trims surrounding whitespace", "  hello world  \n", 100, "hello world"},
		{"long string is tailed", strings.Repeat("a", 600), 500, strings.Repeat("a", 500)},
		{"clean newline boundary keeps full slice", "first line\nsecond line\nthird line", 22, "second line\nthird line"},
		{"mid-line cut advances to next newline", "first line\nsecond line\nthird line", 20, "third line"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tailStderr(tt.in, tt.n); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestChannelManagerService_SessionID(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	var capturedArgs []string
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"ok"}`)
	}

	responseSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "test message",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		responseSent <- struct{}{}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-responseSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	expectedSessionID := "channel-telegram-123"
	found := false
	for i, arg := range capturedArgs {
		if arg == "--session-id" && i+1 < len(capturedArgs) && capturedArgs[i+1] == expectedSessionID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected session ID %q in args, got %v", expectedSessionID, capturedArgs)
	}

	remoteFound := false
	for _, arg := range capturedArgs {
		if arg == "--remote" {
			remoteFound = true
			break
		}
	}
	if !remoteFound {
		t.Errorf("expected --remote flag in args (channel sessions must use the remote-control system prompt), got %v", capturedArgs)
	}
}

func TestWriteSessionImage(t *testing.T) {
	origBaseDir := imageBaseDir
	imageBaseDir = t.TempDir()
	t.Cleanup(func() { imageBaseDir = origBaseDir })

	imgData := []byte("fake-image-bytes")
	b64 := base64.StdEncoding.EncodeToString(imgData)
	sessionID := "test-session-write"

	tests := []struct {
		name    string
		img     domain.ImageAttachment
		wantExt string
	}{
		{"jpeg", domain.ImageAttachment{Data: b64, MimeType: "image/jpeg", Filename: "photo.jpg"}, ".jpg"},
		{"png", domain.ImageAttachment{Data: b64, MimeType: "image/png", Filename: "shot.png"}, ".png"},
		{"gif", domain.ImageAttachment{Data: b64, MimeType: "image/gif"}, ".gif"},
		{"webp", domain.ImageAttachment{Data: b64, MimeType: "image/webp"}, ".webp"},
		{"default", domain.ImageAttachment{Data: b64, MimeType: "image/jpeg"}, ".jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := writeSessionImage(sessionID, tt.img)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if !strings.HasSuffix(path, tt.wantExt) {
				t.Errorf("expected extension %q, got path %q", tt.wantExt, path)
			}

			if !strings.Contains(path, sessionID) {
				t.Errorf("expected path to contain session ID %q, got %q", sessionID, path)
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read image file: %v", err)
			}
			if string(data) != string(imgData) {
				t.Errorf("file content mismatch: got %q, want %q", data, imgData)
			}
		})
	}
}

func TestPruneSessionImages(t *testing.T) {
	origBaseDir := imageBaseDir
	imageBaseDir = t.TempDir()
	t.Cleanup(func() { imageBaseDir = origBaseDir })

	sessionID := "test-session-prune"
	dir := sessionImageDir(sessionID)

	imgData := []byte("fake")
	b64 := base64.StdEncoding.EncodeToString(imgData)

	for i := 0; i < 7; i++ {
		_, err := writeSessionImage(sessionID, domain.ImageAttachment{
			Data:     b64,
			MimeType: "image/jpeg",
			Filename: "img.jpg",
		})
		if err != nil {
			t.Fatalf("failed to write image %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	pruneSessionImages(sessionID, 3)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "infer-") {
			count++
		}
	}

	if count != 3 {
		t.Errorf("expected 3 images after pruning, got %d", count)
	}
}

func TestPruneSessionImages_ZeroRetention(t *testing.T) {
	origBaseDir := imageBaseDir
	imageBaseDir = t.TempDir()
	t.Cleanup(func() { imageBaseDir = origBaseDir })

	sessionID := "test-session-no-prune"
	dir := sessionImageDir(sessionID)

	imgData := []byte("fake")
	b64 := base64.StdEncoding.EncodeToString(imgData)

	for i := 0; i < 3; i++ {
		_, err := writeSessionImage(sessionID, domain.ImageAttachment{
			Data:     b64,
			MimeType: "image/jpeg",
		})
		if err != nil {
			t.Fatalf("failed to write image: %v", err)
		}
	}

	pruneSessionImages(sessionID, 0)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}

	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "infer-") {
			count++
		}
	}

	if count != 3 {
		t.Errorf("expected all 3 images retained, got %d", count)
	}
}

func TestChannelManagerService_ImagePassedToAgent(t *testing.T) {
	origBaseDir := imageBaseDir
	imageBaseDir = t.TempDir()
	t.Cleanup(func() { imageBaseDir = origBaseDir })

	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	var capturedArgs []string
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"I see an image"}`)
	}

	responseSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "what is this?",
			Images: []domain.ImageAttachment{
				{
					Data:     base64.StdEncoding.EncodeToString([]byte("fake-image")),
					MimeType: "image/jpeg",
					Filename: "photo.jpg",
				},
			},
			Timestamp: time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		responseSent <- struct{}{}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-responseSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for response")
	}

	foundFiles := false
	for i, arg := range capturedArgs {
		if arg == "--files" && i+1 < len(capturedArgs) {
			foundFiles = true
			break
		}
	}
	if !foundFiles {
		t.Errorf("expected --files flag in args, got %v", capturedArgs)
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
	req := &domain.ApprovalRequest{
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
	req := &domain.ApprovalRequest{
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

func TestChannelManagerService_ApprovalInterception(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled:         true,
		RequireApproval: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	approvalReq := domain.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"echo hello"}`,
		ToolCallID: "call_1",
	}
	approvalJSON, _ := json.Marshal(approvalReq)
	agentOutput := string(approvalJSON) + "\n" + `{"role":"assistant","content":"Done!"}`

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", agentOutput)
	}

	var messages []domain.OutboundMessage
	allSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "do something",
			Timestamp:   time.Now(),
		}

		time.Sleep(200 * time.Millisecond)
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "yes",
			Timestamp:   time.Now(),
		}

		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		messages = append(messages, msg)
		if len(messages) >= 2 {
			allSent <- struct{}{}
		}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-allSent:
		if !strings.Contains(messages[0].Content, "Bash") {
			t.Errorf("expected approval prompt, got %q", messages[0].Content)
		}
		if messages[1].Content != "Done!" {
			t.Errorf("expected 'Done!', got %q", messages[1].Content)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for messages, got %d messages", len(messages))
	}
}

func TestChannelManagerService_RequireApprovalFlag(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled:         true,
		RequireApproval: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	var capturedArgs []string
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"ok"}`)
	}

	responseSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "test",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		responseSent <- struct{}{}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-responseSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	found := false
	for _, arg := range capturedArgs {
		if arg == "--require-approval" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected --require-approval in args, got %v", capturedArgs)
	}
}

func TestChannelManagerService_NoApprovalFlagByDefault(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	var capturedArgs []string
	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return exec.CommandContext(ctx, "echo", `{"role":"assistant","content":"ok"}`)
	}

	responseSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "test",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		responseSent <- struct{}{}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-responseSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	for _, arg := range capturedArgs {
		if arg == "--require-approval" {
			t.Error("--require-approval should not be present when RequireApproval is false")
		}
	}
}

func TestChannelManagerService_ApprovalMetadataInterception(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled:         true,
		RequireApproval: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	approvalReq := domain.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"echo hello"}`,
		ToolCallID: "call_1",
	}
	approvalJSON, _ := json.Marshal(approvalReq)
	agentOutput := string(approvalJSON) + "\n" + `{"role":"assistant","content":"Done!"}`

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", agentOutput)
	}

	var messages []domain.OutboundMessage
	allSent := make(chan struct{}, 1)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "do something",
			Timestamp:   time.Now(),
		}

		time.Sleep(200 * time.Millisecond)
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "approve",
			Timestamp:   time.Now(),
			Metadata: map[string]string{
				"approval_response": "true",
				"approved":          "true",
				"tool_call_id":      "call_1",
			},
		}

		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		messages = append(messages, msg)
		if len(messages) >= 2 {
			allSent <- struct{}{}
		}
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-allSent:
		if !strings.Contains(messages[0].Content, "Bash") {
			t.Errorf("expected approval prompt, got %q", messages[0].Content)
		}
		if messages[1].Content != "Done!" {
			t.Errorf("expected 'Done!', got %q", messages[1].Content)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for messages, got %d messages", len(messages))
	}
}

// fakeApprovalChannel implements both Channel and ApprovalChannel for testing.
type fakeApprovalChannel struct {
	*fakesdomain.FakeChannel
	sendApprovalCalled bool
	sendApprovalReq    *domain.ApprovalRequest
}

func (f *fakeApprovalChannel) SendApproval(_ context.Context, _ string, req *domain.ApprovalRequest) error {
	f.sendApprovalCalled = true
	f.sendApprovalReq = req
	return nil
}

func TestChannelManagerService_ApprovalWithApprovalChannel(t *testing.T) {
	cfg := config.ChannelsConfig{
		Enabled:         true,
		RequireApproval: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)

	approvalReq := domain.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"echo test"}`,
		ToolCallID: "call_2",
	}
	approvalJSON, _ := json.Marshal(approvalReq)
	agentOutput := string(approvalJSON) + "\n" + `{"role":"assistant","content":"All done!"}`

	cm.execCommandFunc = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "echo", agentOutput)
	}

	var messages []domain.OutboundMessage
	allSent := make(chan struct{}, 1)

	fakeCh := &fakesdomain.FakeChannel{}
	fakeCh.NameReturns("telegram")
	fakeCh.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "run a command",
			Timestamp:   time.Now(),
		}

		time.Sleep(200 * time.Millisecond)
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "approve",
			Timestamp:   time.Now(),
			Metadata: map[string]string{
				"approval_response": "true",
				"approved":          "true",
				"tool_call_id":      "call_2",
			},
		}

		<-ctx.Done()
		return ctx.Err()
	}
	fakeCh.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		messages = append(messages, msg)
		if len(messages) >= 1 {
			allSent <- struct{}{}
		}
		return nil
	}

	approvalCh := &fakeApprovalChannel{FakeChannel: fakeCh}
	cm.Register(approvalCh)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := cm.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case <-allSent:
		if !approvalCh.sendApprovalCalled {
			t.Error("expected SendApproval to be called on ApprovalChannel")
		}
		if approvalCh.sendApprovalReq == nil || approvalCh.sendApprovalReq.ToolName != "Bash" {
			t.Error("expected SendApproval to receive the correct ApprovalRequest")
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("timeout waiting for messages, got %d messages", len(messages))
	}
}

// startAgentTestManager wires a ChannelManagerService with a fake channel and
// a custom execCommandFunc, returning the channel that receives outbound
// messages plus a cancel func. Used by error-propagation tests.
func startAgentTestManager(t *testing.T, exec func(ctx context.Context, name string, args ...string) *exec.Cmd) (chan domain.OutboundMessage, context.CancelFunc) {
	t.Helper()
	cfg := config.ChannelsConfig{
		Enabled: true,
		Telegram: config.TelegramChannelConfig{
			AllowedUsers: []string{"123"},
		},
	}
	cm := NewChannelManagerService(cfg, nil)
	cm.execCommandFunc = exec

	sent := make(chan domain.OutboundMessage, 8)
	ch := &fakesdomain.FakeChannel{}
	ch.NameReturns("telegram")
	ch.StartStub = func(ctx context.Context, inbox chan<- domain.InboundMessage) error {
		inbox <- domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    "123",
			Content:     "trigger",
			Timestamp:   time.Now(),
		}
		<-ctx.Done()
		return ctx.Err()
	}
	ch.SendStub = func(ctx context.Context, msg domain.OutboundMessage) error {
		sent <- msg
		return nil
	}
	cm.Register(ch)

	ctx, cancel := context.WithCancel(context.Background())
	if err := cm.Start(ctx); err != nil {
		cancel()
		t.Fatalf("unexpected start error: %v", err)
	}
	return sent, cancel
}

// collectMessages drains the sent channel until either deadline or noActivity passes.
// Returns all messages observed.
func collectMessages(sent chan domain.OutboundMessage, noActivity time.Duration, deadline time.Duration) []domain.OutboundMessage {
	var msgs []domain.OutboundMessage
	overall := time.After(deadline)
	for {
		select {
		case m := <-sent:
			msgs = append(msgs, m)
		case <-time.After(noActivity):
			return msgs
		case <-overall:
			return msgs
		}
	}
}

func TestRunAgent_ForwardsStructuredError(t *testing.T) {
	sent, cancel := startAgentTestManager(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c",
			`printf '%s\n' '{"type":"agent_error","message":"context length exceeded"}'; exit 1`)
	})
	defer cancel()

	msgs := collectMessages(sent, 500*time.Millisecond, 5*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Error: context length exceeded" {
		t.Errorf("got %q", msgs[0].Content)
	}
}

func TestRunAgent_SafetyNetOnExitWithoutForward(t *testing.T) {
	sent, cancel := startAgentTestManager(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c",
			`printf 'fatal: something broke\n' >&2; exit 1`)
	})
	defer cancel()

	msgs := collectMessages(sent, 500*time.Millisecond, 5*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 safety-net message, got %d: %+v", len(msgs), msgs)
	}
	if !strings.HasPrefix(msgs[0].Content, "Agent failed: ") {
		t.Errorf("expected safety-net prefix, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[0].Content, "fatal: something broke") {
		t.Errorf("expected stderr tail in message, got %q", msgs[0].Content)
	}
}

func TestRunAgent_SafetyNetOnExitWithEmptyStderr(t *testing.T) {
	sent, cancel := startAgentTestManager(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	})
	defer cancel()

	msgs := collectMessages(sent, 500*time.Millisecond, 5*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Agent failed: unknown error" {
		t.Errorf("got %q", msgs[0].Content)
	}
}

func TestRunAgent_NoDoubleSendOnStructuredError(t *testing.T) {
	sent, cancel := startAgentTestManager(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c",
			`printf '%s\n' '{"type":"agent_error","message":"boom"}'; printf 'noisy stderr\n' >&2; exit 1`)
	})
	defer cancel()

	msgs := collectMessages(sent, 500*time.Millisecond, 5*time.Second)
	if len(msgs) != 1 {
		t.Fatalf("expected exactly 1 message (no safety-net duplicate), got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Content != "Error: boom" {
		t.Errorf("got %q", msgs[0].Content)
	}
}
