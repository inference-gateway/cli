package channels

import (
	"context"
	"encoding/json"
	"fmt"
	ipc "github.com/inference-gateway/cli/internal/platform/ipc"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	bot "github.com/go-telegram/bot"
	models "github.com/go-telegram/bot/models"

	config "github.com/inference-gateway/cli/config"
)

func TestTelegramChannel_Name(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	if ch.Name() != "telegram" {
		t.Errorf("expected 'telegram', got %q", ch.Name())
	}
}

func TestTelegramChannel_StartRequiresToken(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{
		BotToken: "",
	}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inbox := make(chan InboundMessage, 10)
	err := ch.Start(ctx, inbox)

	if err == nil || !strings.Contains(err.Error(), "bot token is required") {
		t.Errorf("expected bot token error, got %v", err)
	}
}

func TestProcessUpdate_TextMessage(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID: 42,
			From: &models.User{
				ID:        123,
				FirstName: "Test",
				Username:  "testuser",
			},
			Chat: models.Chat{
				ID:   123,
				Type: "private",
			},
			Date: int(time.Now().Unix()),
			Text: "hello",
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}

	if msg.Content != "hello" {
		t.Errorf("expected content 'hello', got %q", msg.Content)
	}
	if msg.ChannelName != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", msg.ChannelName)
	}
	if msg.SenderID != "123" {
		t.Errorf("expected sender '123', got %q", msg.SenderID)
	}
	if msg.Metadata["username"] != "testuser" {
		t.Errorf("expected username 'testuser', got %q", msg.Metadata["username"])
	}
}

func TestProcessUpdate_VideoFilteredWhenMediaDisabled(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 123, Type: "private"},
			Date: int(time.Now().Unix()),
			Video: &models.Video{
				FileID: "video123",
			},
			Caption: "my video",
		},
	}

	msg := processUpdate(update, false)
	if msg != nil {
		t.Fatal("expected nil message for video when media saving is disabled")
	}
}

func TestProcessUpdate_VideoWhenMediaEnabled(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 123, Type: "private"},
			Date: int(time.Now().Unix()),
			Video: &models.Video{
				FileID:   "video123",
				MimeType: "video/mp4",
				FileSize: 1024,
			},
		},
	}

	msg := processUpdate(update, true)
	if msg == nil {
		t.Fatal("expected non-nil message for video when media saving is enabled")
	}
	if msg.Content != "[Attached video]" {
		t.Errorf("expected placeholder content '[Attached video]', got %q", msg.Content)
	}
	if msg.Metadata["media_file_id"] != "video123" {
		t.Errorf("expected media_file_id 'video123', got %q", msg.Metadata["media_file_id"])
	}
	if msg.Metadata["media_mime"] != "video/mp4" {
		t.Errorf("expected media_mime 'video/mp4', got %q", msg.Metadata["media_mime"])
	}
	if msg.Metadata["media_size"] != "1024" {
		t.Errorf("expected media_size '1024', got %q", msg.Metadata["media_size"])
	}
}

func TestProcessUpdate_VideoDefaultsMime(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:      42,
			Chat:    models.Chat{ID: 123, Type: "private"},
			Date:    int(time.Now().Unix()),
			Video:   &models.Video{FileID: "video123"},
			Caption: "my video",
		},
	}

	msg := processUpdate(update, true)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Content != "my video" {
		t.Errorf("expected caption as content, got %q", msg.Content)
	}
	if msg.Metadata["media_mime"] != "video/mp4" {
		t.Errorf("expected media_mime to default to 'video/mp4', got %q", msg.Metadata["media_mime"])
	}
}

func TestProcessUpdate_PhotoWithCaption(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:      42,
			Chat:    models.Chat{ID: 123, Type: "private"},
			Date:    int(time.Now().Unix()),
			Caption: "check this image",
			Photo: []models.PhotoSize{
				{FileID: "small", Width: 100, Height: 100},
				{FileID: "large", Width: 800, Height: 600},
			},
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message for photo")
	}

	if msg.Content != "check this image" {
		t.Errorf("expected caption as content, got %q", msg.Content)
	}
	if msg.Metadata["photo_file_id"] != "large" {
		t.Errorf("expected largest photo file_id, got %q", msg.Metadata["photo_file_id"])
	}
}

func TestProcessUpdate_PhotoWithoutCaption(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 123, Type: "private"},
			Date: int(time.Now().Unix()),
			Photo: []models.PhotoSize{
				{FileID: "photo123", Width: 800, Height: 600},
			},
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message for photo without caption")
	}

	if msg.Content != "[Attached image]" {
		t.Errorf("expected fallback content '[Attached image]', got %q", msg.Content)
	}
	if msg.Metadata["photo_file_id"] != "photo123" {
		t.Errorf("expected photo_file_id 'photo123', got %q", msg.Metadata["photo_file_id"])
	}
}

func TestProcessUpdate_VoiceMessage(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:    42,
			Chat:  models.Chat{ID: 123, Type: "private"},
			Date:  int(time.Now().Unix()),
			Voice: &models.Voice{FileID: "voice123", Duration: 3},
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message for voice")
	}
	if msg.Metadata["voice_file_id"] != "voice123" {
		t.Errorf("expected voice_file_id 'voice123', got %q", msg.Metadata["voice_file_id"])
	}
	if msg.Content != "[Voice message]" {
		t.Errorf("expected placeholder content '[Voice message]', got %q", msg.Content)
	}
}

func TestProcessUpdate_AudioMessage(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:    42,
			Chat:  models.Chat{ID: 123, Type: "private"},
			Date:  int(time.Now().Unix()),
			Audio: &models.Audio{FileID: "audio123", Duration: 5},
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message for audio")
	}
	if msg.Metadata["voice_file_id"] != "audio123" {
		t.Errorf("expected voice_file_id 'audio123', got %q", msg.Metadata["voice_file_id"])
	}
}

func TestApplyVoiceTranscription_DisabledDropsMessage(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	msg := &InboundMessage{Content: "[Voice message]"}
	if ch.applyVoiceTranscription(context.Background(), nil, msg, "voice123") {
		t.Error("expected applyVoiceTranscription to return false when transcriber is nil")
	}
}

func TestProcessUpdate_NilMessage(t *testing.T) {
	update := &models.Update{
		ID:      1,
		Message: nil,
	}

	msg := processUpdate(update, false)
	if msg != nil {
		t.Fatal("expected nil for nil message")
	}
}

func TestProcessUpdate_EmptyMessage(t *testing.T) {
	update := &models.Update{
		ID: 1,
		Message: &models.Message{
			ID:   42,
			Chat: models.Chat{ID: 123, Type: "private"},
			Date: int(time.Now().Unix()),
		},
	}

	msg := processUpdate(update, false)
	if msg != nil {
		t.Fatal("expected nil for empty message")
	}
}

func TestSplitMessage(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLen   int
		expected int
	}{
		{"short message", "hello", 100, 1},
		{"exact limit", strings.Repeat("a", 100), 100, 1},
		{"needs split", strings.Repeat("a", 150), 100, 2},
		{"empty", "", 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := splitMessage(tt.text, tt.maxLen)
			if len(chunks) != tt.expected {
				t.Errorf("expected %d chunks, got %d", tt.expected, len(chunks))
			}

			joined := strings.Join(chunks, "")
			if joined != tt.text {
				t.Error("content not preserved after splitting")
			}
		})
	}
}

func TestSplitMessage_SplitsAtNewline(t *testing.T) {
	text := strings.Repeat("a", 60) + "\n" + strings.Repeat("b", 50)
	chunks := splitMessage(text, 100)

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if !strings.HasSuffix(chunks[0], "\n") {
		t.Error("expected first chunk to end at newline boundary")
	}
}

func TestMimeFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"photos/image.jpg", "image/jpeg"},
		{"photos/image.jpeg", "image/jpeg"},
		{"photos/image.png", "image/png"},
		{"photos/image.gif", "image/gif"},
		{"photos/image.webp", "image/webp"},
		{"photos/image.JPG", "image/jpeg"},
		{"photos/image.PNG", "image/png"},
		{"photos/unknown", "image/jpeg"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := mimeFromPath(tt.path)
			if got != tt.want {
				t.Errorf("mimeFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// fileServer serves getFile (resolving any file_id to filePath) and the file
// download endpoint (returning content), so fetchTelegramFile can be exercised
// end to end against a local server.
func fileServer(t *testing.T, filePath string, content []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"file_id":"x","file_path":%q}}`, filePath)
		case strings.Contains(r.URL.Path, "/file/bot"):
			_, _ = w.Write(content)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
}

func TestFetchTelegramFile(t *testing.T) {
	srv := fileServer(t, "photos/file_1.jpg", []byte("jpeg-bytes"))
	defer srv.Close()
	ch := newTestChannel(t, srv.URL)

	data, filePath, err := fetchTelegramFile(context.Background(), ch.bot, "photo123")
	if err != nil {
		t.Fatalf("fetchTelegramFile: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("data = %q, want jpeg-bytes", data)
	}
	if filePath != "photos/file_1.jpg" {
		t.Errorf("filePath = %q, want photos/file_1.jpg", filePath)
	}
}

func TestMediaAcceptable(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.TelegramMediaConfig
		mime    string
		size    int64
		wantErr bool
	}{
		{"defaults allow small mp4", config.TelegramMediaConfig{}, "video/mp4", 1 << 20, false},
		{"defaults allow jpeg", config.TelegramMediaConfig{}, "image/jpeg", 1 << 20, false},
		{"defaults reject over 10MB", config.TelegramMediaConfig{}, "video/mp4", 11 << 20, true},
		{"defaults reject unknown mime", config.TelegramMediaConfig{}, "application/x-msdownload", 100, true},
		{"custom max size", config.TelegramMediaConfig{MaxSizeMB: 1}, "video/mp4", 2 << 20, true},
		{"custom allowlist rejects mp4", config.TelegramMediaConfig{AllowedMimeTypes: []string{"image/png"}}, "video/mp4", 100, true},
		{"custom allowlist accepts png", config.TelegramMediaConfig{AllowedMimeTypes: []string{"image/png"}}, "image/png", 100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewTelegramChannel(config.TelegramChannelConfig{Media: tt.cfg}, nil, nil)
			err := ch.mediaAcceptable(tt.mime, tt.size)
			if (err != nil) != tt.wantErr {
				t.Errorf("mediaAcceptable(%q, %d) err = %v, wantErr %v", tt.mime, tt.size, err, tt.wantErr)
			}
		})
	}
}

// newMediaChannel returns a channel with media saving enabled into a temp dir.
func newMediaChannel(t *testing.T, cfg config.TelegramMediaConfig) *TelegramChannel {
	t.Helper()
	cfg.Enabled = true
	if cfg.Dir == "" {
		cfg.Dir = t.TempDir()
	}
	return NewTelegramChannel(config.TelegramChannelConfig{Media: cfg}, nil, nil)
}

func TestSaveInboundMedia(t *testing.T) {
	ch := newMediaChannel(t, config.TelegramMediaConfig{})
	msg := &InboundMessage{Content: "[Attached image]", Metadata: map[string]string{}}

	ch.saveInboundMedia(msg, "image/jpeg", "photos/file_1.jpg", []byte("jpeg-bytes"))

	path := msg.Metadata["media_path"]
	if path == "" {
		t.Fatal("expected media_path metadata to be set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file unreadable: %v", err)
	}
	if string(data) != "jpeg-bytes" {
		t.Errorf("saved content = %q, want jpeg-bytes", data)
	}
	if filepath.Ext(path) != ".jpg" {
		t.Errorf("saved file extension = %q, want .jpg", filepath.Ext(path))
	}
	if !strings.Contains(msg.Content, "[Attachment saved: "+path+"]") {
		t.Errorf("expected content to mention saved path, got %q", msg.Content)
	}
}

func TestSaveInboundMedia_RejectsDisallowedMime(t *testing.T) {
	ch := newMediaChannel(t, config.TelegramMediaConfig{AllowedMimeTypes: []string{"image/png"}})
	dir := ch.media.Dir
	msg := &InboundMessage{Content: "x", Metadata: map[string]string{}}

	ch.saveInboundMedia(msg, "image/jpeg", "photos/file_1.jpg", []byte("jpeg-bytes"))

	if msg.Metadata["media_path"] != "" {
		t.Errorf("expected no media_path for disallowed mime, got %q", msg.Metadata["media_path"])
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected nothing written for disallowed mime, got %d entries", len(entries))
	}
}

func TestSaveInboundMedia_DisabledIsNoOp(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	msg := &InboundMessage{Content: "x", Metadata: map[string]string{}}

	ch.saveInboundMedia(msg, "image/jpeg", "photos/file_1.jpg", []byte("jpeg-bytes"))

	if msg.Content != "x" || msg.Metadata["media_path"] != "" {
		t.Errorf("expected message untouched when media disabled, got %+v", msg)
	}
}

func TestDownloadInboundVideo_SavesFile(t *testing.T) {
	srv := fileServer(t, "videos/file_7.mp4", []byte("mp4-bytes"))
	defer srv.Close()

	ch := newMediaChannel(t, config.TelegramMediaConfig{})
	b, err := bot.New("test-token", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	ch.bot = b

	msg := &InboundMessage{
		Content: "[Attached video]",
		Metadata: map[string]string{
			"media_file_id": "video123",
			"media_mime":    "video/mp4",
			"media_size":    "9",
		},
	}
	ch.downloadInboundVideo(context.Background(), b, msg, "video123")

	path := msg.Metadata["media_path"]
	if path == "" {
		t.Fatalf("expected media_path metadata, content: %q", msg.Content)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("saved file unreadable: %v", err)
	}
	if string(data) != "mp4-bytes" {
		t.Errorf("saved content = %q, want mp4-bytes", data)
	}
	if filepath.Ext(path) != ".mp4" {
		t.Errorf("saved file extension = %q, want .mp4", filepath.Ext(path))
	}
}

func TestDownloadInboundVideo_RejectsOversizeBeforeDownload(t *testing.T) {
	var downloads int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/file/bot") {
			downloads++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"x","file_path":"videos/file_7.mp4"}}`))
	}))
	defer srv.Close()

	ch := newMediaChannel(t, config.TelegramMediaConfig{MaxSizeMB: 10})
	b, err := bot.New("test-token", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	ch.bot = b

	msg := &InboundMessage{
		Content: "[Attached video]",
		Metadata: map[string]string{
			"media_file_id": "video123",
			"media_mime":    "video/mp4",
			"media_size":    strconv.FormatInt(11<<20, 10),
		},
	}
	ch.downloadInboundVideo(context.Background(), b, msg, "video123")

	if downloads != 0 {
		t.Errorf("expected no download attempt for oversized video, got %d", downloads)
	}
	if msg.Metadata["media_path"] != "" {
		t.Errorf("expected no media_path, got %q", msg.Metadata["media_path"])
	}
	if !strings.Contains(msg.Content, "[Attachment rejected:") {
		t.Errorf("expected rejection note in content, got %q", msg.Content)
	}
}

func TestTelegramChannel_SendRequiresBot(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)

	err := ch.Send(context.Background(), OutboundMessage{
		RecipientID: "123",
		Content:     "test",
	})

	if err == nil || !strings.Contains(err.Error(), "bot not started") {
		t.Errorf("expected 'bot not started' error, got %v", err)
	}
}

func TestTelegramChannel_Stop(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	err := ch.Stop()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestTelegramChannel_SendApprovalRequiresBot(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)

	err := ch.SendApproval(context.Background(), "123", &ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"ls"}`,
		ToolCallID: "call_1",
	})

	if err == nil || !strings.Contains(err.Error(), "bot not started") {
		t.Errorf("expected 'bot not started' error, got %v", err)
	}
}

func TestProcessCallbackQuery_Approve(t *testing.T) {
	cq := &models.CallbackQuery{
		ID:   "cq_1",
		From: models.User{ID: 456},
		Message: models.MaybeInaccessibleMessage{
			Message: &models.Message{
				Chat: models.Chat{ID: 123},
			},
		},
		Data: "approve:call_abc123",
	}

	msg := processCallbackQuery(cq)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}

	if msg.ChannelName != "telegram" {
		t.Errorf("expected channel 'telegram', got %q", msg.ChannelName)
	}
	if msg.SenderID != "123" {
		t.Errorf("expected sender '123', got %q", msg.SenderID)
	}
	if msg.Content != "approve" {
		t.Errorf("expected content 'approve', got %q", msg.Content)
	}
	if msg.Metadata["approval_response"] != "true" {
		t.Error("expected approval_response metadata to be 'true'")
	}
	if msg.Metadata["approved"] != "true" {
		t.Error("expected approved metadata to be 'true'")
	}
	if msg.Metadata["tool_call_id"] != "call_abc123" {
		t.Errorf("expected tool_call_id 'call_abc123', got %q", msg.Metadata["tool_call_id"])
	}
	if msg.Metadata["callback_query_id"] != "cq_1" {
		t.Errorf("expected callback_query_id 'cq_1', got %q", msg.Metadata["callback_query_id"])
	}
}

func TestProcessCallbackQuery_Reject(t *testing.T) {
	cq := &models.CallbackQuery{
		ID:   "cq_2",
		From: models.User{ID: 456},
		Message: models.MaybeInaccessibleMessage{
			Message: &models.Message{
				Chat: models.Chat{ID: 123},
			},
		},
		Data: "reject:call_xyz",
	}

	msg := processCallbackQuery(cq)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}

	if msg.Metadata["approved"] != "false" {
		t.Error("expected approved metadata to be 'false'")
	}
	if msg.Content != "reject" {
		t.Errorf("expected content 'reject', got %q", msg.Content)
	}
}

func TestProcessCallbackQuery_InvalidFormat(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"no colon", "approveall"},
		{"unknown action", "delete:call_1"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cq := &models.CallbackQuery{
				ID:   "cq_3",
				From: models.User{ID: 456},
				Message: models.MaybeInaccessibleMessage{
					Message: &models.Message{
						Chat: models.Chat{ID: 123},
					},
				},
				Data: tt.data,
			}
			msg := processCallbackQuery(cq)
			if msg != nil {
				t.Errorf("expected nil for data %q, got %+v", tt.data, msg)
			}
		})
	}
}

func TestProcessCallbackQuery_NilMessage(t *testing.T) {
	cq := &models.CallbackQuery{
		ID:      "cq_4",
		From:    models.User{ID: 456},
		Message: models.MaybeInaccessibleMessage{},
		Data:    "approve:call_1",
	}

	msg := processCallbackQuery(cq)
	if msg != nil {
		t.Error("expected nil when both Message and InaccessibleMessage are nil")
	}
}

func TestProcessCallbackQuery_InaccessibleMessage(t *testing.T) {
	cq := &models.CallbackQuery{
		ID:   "cq_5",
		From: models.User{ID: 456},
		Message: models.MaybeInaccessibleMessage{
			InaccessibleMessage: &models.InaccessibleMessage{
				Chat: models.Chat{ID: 789},
			},
		},
		Data: "approve:call_99",
	}

	msg := processCallbackQuery(cq)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.SenderID != "789" {
		t.Errorf("expected sender '789', got %q", msg.SenderID)
	}
}

func TestProcessUpdate_CallbackQuery(t *testing.T) {
	update := &models.Update{
		CallbackQuery: &models.CallbackQuery{
			ID:   "cq_6",
			From: models.User{ID: 456},
			Message: models.MaybeInaccessibleMessage{
				Message: &models.Message{
					Chat: models.Chat{ID: 123},
				},
			},
			Data: "reject:call_42",
		},
	}

	msg := processUpdate(update, false)
	if msg == nil {
		t.Fatal("expected non-nil message for callback query")
	}
	if msg.Metadata["approval_response"] != "true" {
		t.Error("expected approval_response metadata")
	}
	if msg.Metadata["approved"] != "false" {
		t.Error("expected approved to be 'false' for reject")
	}
}

func TestFormatApprovalText(t *testing.T) {
	req := &ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Bash",
		ToolArgs:   `{"command":"ls -la"}`,
		ToolCallID: "call_1",
	}

	text := formatApprovalText(req)

	if !strings.Contains(text, "Bash") {
		t.Error("expected text to contain tool name")
	}
	if !strings.Contains(text, "ls -la") {
		t.Error("expected text to contain command")
	}
	if strings.Contains(text, "yes") || strings.Contains(text, "no") {
		t.Error("expected text NOT to contain yes/no instructions")
	}
}

func TestFormatApprovalText_FilePath(t *testing.T) {
	req := &ipc.ApprovalRequest{
		Type:       "approval_request",
		ToolName:   "Write",
		ToolArgs:   `{"file_path":"/tmp/test.txt"}`,
		ToolCallID: "call_2",
	}

	text := formatApprovalText(req)

	if !strings.Contains(text, "/tmp/test.txt") {
		t.Error("expected text to contain file path")
	}
}

func TestExtractImagePaths(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(img, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big.png")
	if err := os.WriteFile(big, make([]byte, maxPhotoBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".infer", "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".infer", "tmp", "rel.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	tests := []struct {
		name      string
		content   string
		wantPaths []string
		wantText  string
	}{
		{
			name:      "img tag with file scheme is stripped",
			content:   `Here: <img src="file://` + img + `" alt="x" width="600"/> done`,
			wantPaths: []string{img},
			wantText:  "Here:  done",
		},
		{
			name:      "markdown image is stripped",
			content:   "![shot](" + img + ")",
			wantPaths: []string{img},
			wantText:  "",
		},
		{
			name:      "bare path kept in text but sent",
			content:   "Saved to " + img + " (145 KB)",
			wantPaths: []string{img},
			wantText:  "Saved to " + img + " (145 KB)",
		},
		{
			name:      "relative .infer path is extracted",
			content:   "Saved!\n\n![meme](.infer/tmp/rel.png)",
			wantPaths: []string{".infer/tmp/rel.png"},
			wantText:  "Saved!\n\n",
		},
		{
			name:      "missing file left untouched",
			content:   `<img src="/nope/missing.png"/>`,
			wantPaths: nil,
			wantText:  `<img src="/nope/missing.png"/>`,
		},
		{
			name:      "oversize file skipped",
			content:   "see " + big,
			wantPaths: nil,
			wantText:  "see " + big,
		},
		{
			name:      "duplicate references deduped",
			content:   "![a](" + img + ") and " + img,
			wantPaths: []string{img},
			wantText:  " and " + img,
		},
		{
			name:      "image inside code fence is not sent",
			content:   "```\nResult of tool call: {\"saved_path\":\"" + img + "\",\"hint\":\"![image](" + img + ")\"}\n```",
			wantPaths: nil,
			wantText:  "```\nResult of tool call: {\"saved_path\":\"" + img + "\",\"hint\":\"![image](" + img + ")\"}\n```",
		},
		{
			name:      "fenced path ignored but unfenced assistant image sent",
			content:   "```\nsaved to " + img + "\n```\nHere it is:\n![shot](" + img + ")",
			wantPaths: []string{img},
			wantText:  "```\nsaved to " + img + "\n```\nHere it is:\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, text := extractImagePaths(tt.content)
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tt.wantPaths)
			}
			if text != tt.wantText {
				t.Errorf("text = %q, want %q", text, tt.wantText)
			}
		})
	}
}

func TestRenderTelegramHTML(t *testing.T) {
	tests := []struct {
		name string
		md   string
		want string
	}{
		{"bold", "**hi**", "<b>hi</b>"},
		{"inline code", "run `ls -la` now", "run <code>ls -la</code> now"},
		{"header", "# Title\nbody", "<b>Title</b>\nbody"},
		{"fenced block", "before\n```bash\nls -la\n```\nafter", "before\n<pre>ls -la</pre>\nafter"},
		{"html escaped", "a < b & c", "a &lt; b &amp; c"},
		{"escape inside fence", "```\n<img>\n```", "<pre>&lt;img&gt;</pre>"},
		{"plain text untouched", "hello world", "hello world"},
		{
			"quoted prose collapses to expandable blockquote",
			"look:\n> line one\n> line two\ndone",
			"look:\n<blockquote expandable>line one\nline two</blockquote>\ndone",
		},
		{
			"quoted fence renders code (not pre) inside blockquote",
			"> ⚠️ Tool failed:\n> ```\n> exit 502\n> ```",
			"<blockquote expandable>⚠️ Tool failed:\n<code>exit 502</code></blockquote>",
		},
		{
			"header and aligned table",
			"### Tool Calls\n\n| Tool | Calls |\n|------|-------|\n| Bash | 4 |\n| Tree | 3 |",
			"<b>Tool Calls</b>\n\n<pre>Tool  Calls\nBash  4\nTree  3</pre>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderTelegramHTML(tt.md); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTrackMessageCap(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	for i := range maxTrackedMessages + 10 {
		ch.trackMessage(1, i)
	}
	ids := ch.msgIDs[1]
	if len(ids) != maxTrackedMessages {
		t.Fatalf("expected %d tracked IDs, got %d", maxTrackedMessages, len(ids))
	}
	if ids[0] != 10 {
		t.Fatalf("expected oldest IDs dropped, first is %d", ids[0])
	}
}

func TestClearHistoryBatching(t *testing.T) {
	var mu sync.Mutex
	var batches [][]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/deleteMessages") {
			var params struct {
				MessageIDs []any `json:"message_ids"`
			}
			body, _ := io.ReadAll(r.Body)
			ids := r.FormValue("message_ids")
			if ids != "" {
				var parsed []any
				_ = json.Unmarshal([]byte(ids), &parsed)
				params.MessageIDs = parsed
			} else {
				_ = json.Unmarshal(body, &params)
			}
			mu.Lock()
			batches = append(batches, params.MessageIDs)
			failFirst := len(batches) == 1
			mu.Unlock()
			if failFirst {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"test failure"}`))
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	b, err := bot.New("test-token", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	ch := NewTelegramChannel(config.TelegramChannelConfig{BotToken: "test-token"}, nil, nil)
	ch.bot = b
	for i := range 250 {
		ch.trackMessage(99, i)
	}

	if err := ch.ClearHistory(context.Background(), "99"); err != nil {
		t.Fatalf("ClearHistory returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 3 {
		t.Fatalf("expected 3 deleteMessages batches for 250 IDs, got %d", len(batches))
	}
	for i, batch := range batches {
		if len(batch) > deleteBatchSize {
			t.Fatalf("batch %d has %d IDs, exceeds %d", i, len(batch), deleteBatchSize)
		}
	}
	if len(ch.msgIDs[99]) != 0 {
		t.Fatal("expected tracked IDs to be dropped after wipe")
	}
}

func TestClearHistoryRequiresBot(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{}, nil, nil)
	if err := ch.ClearHistory(context.Background(), "1"); err == nil {
		t.Fatal("expected error when bot not started")
	}
}

func TestTelegramBotCommands(t *testing.T) {
	long := strings.Repeat("d", 300)
	cmds := telegramBotCommands([]ChannelCommand{
		{Name: "clear", Description: "ok"},
		{Name: "Bad-Name", Description: "dropped"},
		{Name: "UPPER", Description: "dropped"},
		{Name: strings.Repeat("x", 33), Description: "dropped"},
		{Name: "release-notes", Description: "dropped"},
		{Name: "longdesc", Description: long},
	})
	if len(cmds) != 2 {
		t.Fatalf("expected 2 valid commands, got %d: %v", len(cmds), cmds)
	}
	if cmds[0].Command != "clear" || cmds[1].Command != "longdesc" {
		t.Fatalf("unexpected commands: %v", cmds)
	}
	if len(cmds[1].Description) != maxCommandDescLen {
		t.Fatalf("expected description truncated to %d, got %d", maxCommandDescLen, len(cmds[1].Description))
	}
}

func TestProcessCallbackQuery_CommandButton(t *testing.T) {
	cq := &models.CallbackQuery{
		ID:   "cq_3",
		From: models.User{ID: 456},
		Message: models.MaybeInaccessibleMessage{
			Message: &models.Message{
				Chat: models.Chat{ID: 123},
			},
		},
		Data: "/conversations 68dd338f-487a-49a8-b8a0-019e222e7771",
	}

	msg := processCallbackQuery(cq)
	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Content != cq.Data {
		t.Errorf("expected content %q, got %q", cq.Data, msg.Content)
	}
	if msg.SenderID != "123" {
		t.Errorf("expected sender '123', got %q", msg.SenderID)
	}
	if len(msg.Metadata) != 0 {
		t.Errorf("command buttons must not carry approval metadata, got %v", msg.Metadata)
	}
}

func TestSendWithButtons(t *testing.T) {
	var mu sync.Mutex
	var markups []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			mu.Lock()
			markups = append(markups, r.FormValue("reply_markup"))
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":99}}}`))
	}))
	defer srv.Close()

	b, err := bot.New("test-token", bot.WithServerURL(srv.URL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	ch := NewTelegramChannel(config.TelegramChannelConfig{BotToken: "test-token"}, nil, nil)
	ch.bot = b

	err = ch.Send(context.Background(), OutboundMessage{
		RecipientID: "99",
		Content:     "Tap a conversation to switch:",
		Buttons: []MessageButton{
			{Text: "Weather talk · 4 msgs", Data: "/conversations abc"},
			{Text: "Trip planning · 9 msgs", Data: "/conversations def"},
		},
	})
	if err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(markups) != 1 {
		t.Fatalf("expected 1 sendMessage call, got %d", len(markups))
	}
	var kb struct {
		InlineKeyboard [][]struct {
			Text         string `json:"text"`
			CallbackData string `json:"callback_data"`
		} `json:"inline_keyboard"`
	}
	if err := json.Unmarshal([]byte(markups[0]), &kb); err != nil {
		t.Fatalf("reply_markup is not an inline keyboard: %v (raw: %q)", err, markups[0])
	}
	if len(kb.InlineKeyboard) != 2 || len(kb.InlineKeyboard[0]) != 1 {
		t.Fatalf("expected 2 one-button rows, got %+v", kb.InlineKeyboard)
	}
	if kb.InlineKeyboard[0][0].CallbackData != "/conversations abc" {
		t.Fatalf("unexpected callback data: %+v", kb.InlineKeyboard[0][0])
	}
}

// sendMessageRecorder is an httptest handler that records every sendMessage
// call's parse_mode and returns a scripted response per call index, letting a
// test assert whether sendText retried.
func sendMessageRecorder(t *testing.T, responses []string) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var parseModes []string
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
			return
		}
		mu.Lock()
		parseModes = append(parseModes, r.FormValue("parse_mode"))
		body := responses[min(i, len(responses)-1)]
		i++
		mu.Unlock()
		_, _ = w.Write([]byte(body))
	}))
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), parseModes...)
	}
}

func newTestChannel(t *testing.T, serverURL string) *TelegramChannel {
	t.Helper()
	b, err := bot.New("test-token", bot.WithServerURL(serverURL), bot.WithSkipGetMe())
	if err != nil {
		t.Fatal(err)
	}
	ch := NewTelegramChannel(config.TelegramChannelConfig{BotToken: "test-token"}, nil, nil)
	ch.bot = b
	return ch
}

// TestSendText_NoRetryOnNon400 is the double-message regression guard: a non-400
// error (here 429) must NOT trigger the plain-text fallback, because the first
// send may already have been delivered and a resend would show a duplicate.
func TestSendText_NoRetryOnNon400(t *testing.T) {
	srv, parseModes := sendMessageRecorder(t, []string{
		`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`,
	})
	defer srv.Close()
	ch := newTestChannel(t, srv.URL)

	err := ch.Send(context.Background(), OutboundMessage{RecipientID: "99", Content: "hi"})
	if err == nil {
		t.Fatal("expected Send to return the 429 error")
	}
	if got := parseModes(); len(got) != 1 {
		t.Fatalf("expected exactly 1 sendMessage call (no duplicate), got %d: %v", len(got), got)
	}
}

// TestSendText_RetriesPlainOn400 verifies a 400 parse error DOES fall back to a
// plain-text resend (nothing was delivered on a 400, so the resend is safe and
// necessary), and that the retry drops parse_mode=HTML.
func TestSendText_RetriesPlainOn400(t *testing.T) {
	srv, parseModes := sendMessageRecorder(t, []string{
		`{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities"}`,
		`{"ok":true,"result":{"message_id":1,"chat":{"id":99}}}`,
	})
	defer srv.Close()
	ch := newTestChannel(t, srv.URL)

	if err := ch.Send(context.Background(), OutboundMessage{RecipientID: "99", Content: "hi"}); err != nil {
		t.Fatalf("expected Send to succeed after plain-text fallback, got %v", err)
	}
	got := parseModes()
	if len(got) != 2 {
		t.Fatalf("expected 2 sendMessage calls (HTML then plain), got %d: %v", len(got), got)
	}
	if got[0] != "HTML" {
		t.Fatalf("expected first attempt parse_mode=HTML, got %q", got[0])
	}
	if got[1] != "" {
		t.Fatalf("expected retry to be plain text (no parse_mode), got %q", got[1])
	}
}

// TestSendImage_FallsBackToDocumentOnPhotoError verifies that when Telegram
// rejects an inline photo (e.g. PHOTO_INVALID_DIMENSIONS for an over-tall
// full-page screenshot), Send retries the upload as a document so the image is
// still delivered instead of silently dropped.
func TestSendImage_FallsBackToDocumentOnPhotoError(t *testing.T) {
	imgPath := filepath.Join(t.TempDir(), "fullpage.png")
	if err := os.WriteFile(imgPath, []byte("real-file-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var photo, document int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendPhoto"):
			mu.Lock()
			photo++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: PHOTO_INVALID_DIMENSIONS"}`))
		case strings.HasSuffix(r.URL.Path, "/sendDocument"):
			mu.Lock()
			document++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7,"chat":{"id":99}}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
		}
	}))
	defer srv.Close()

	ch := newTestChannel(t, srv.URL)
	if err := ch.Send(context.Background(), OutboundMessage{
		RecipientID: "99",
		Content:     "![image](" + imgPath + ")",
	}); err != nil {
		t.Fatalf("Send returned error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if photo != 1 {
		t.Fatalf("expected exactly 1 sendPhoto attempt, got %d", photo)
	}
	if document != 1 {
		t.Fatalf("expected sendDocument fallback after photo rejection, got %d", document)
	}
}

func TestInlineKeyboard_TruncatesLabels(t *testing.T) {
	if inlineKeyboard(nil) != nil {
		t.Fatal("expected nil markup for no buttons")
	}
	kb := inlineKeyboard([]MessageButton{{Text: strings.Repeat("é", 60), Data: "/x"}})
	markup, ok := kb.(*models.InlineKeyboardMarkup)
	if !ok {
		t.Fatalf("expected InlineKeyboardMarkup, got %T", kb)
	}
	label := markup.InlineKeyboard[0][0].Text
	if r := []rune(label); len(r) != maxButtonTextLen || r[len(r)-1] != '…' {
		t.Fatalf("expected %d-rune label ending in ellipsis, got %q", maxButtonTextLen, label)
	}
}
