package channels

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	models "github.com/go-telegram/bot/models"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestTelegramChannel_Name(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{})
	if ch.Name() != "telegram" {
		t.Errorf("expected 'telegram', got %q", ch.Name())
	}
}

func TestTelegramChannel_StartRequiresToken(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{
		BotToken: "",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	inbox := make(chan domain.InboundMessage, 10)
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

	msg := processUpdate(update)
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

func TestProcessUpdate_VideoFiltered(t *testing.T) {
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

	msg := processUpdate(update)
	if msg != nil {
		t.Fatal("expected nil message for video")
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

	msg := processUpdate(update)
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

	msg := processUpdate(update)
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

func TestProcessUpdate_NilMessage(t *testing.T) {
	update := &models.Update{
		ID:      1,
		Message: nil,
	}

	msg := processUpdate(update)
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

	msg := processUpdate(update)
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

func TestDownloadTelegramPhoto(t *testing.T) {
	imageData := []byte("fake-png-data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/file/bot") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		if _, err := w.Write(imageData); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer srv.Close()

	_ = downloadTelegramPhoto
}

func TestTelegramChannel_SendRequiresBot(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{})

	err := ch.Send(context.Background(), domain.OutboundMessage{
		RecipientID: "123",
		Content:     "test",
	})

	if err == nil || !strings.Contains(err.Error(), "bot not started") {
		t.Errorf("expected 'bot not started' error, got %v", err)
	}
}

func TestTelegramChannel_Stop(t *testing.T) {
	ch := NewTelegramChannel(config.TelegramChannelConfig{})
	err := ch.Stop()
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}
