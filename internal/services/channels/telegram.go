package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	bot "github.com/go-telegram/bot"
	models "github.com/go-telegram/bot/models"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

const maxMessageLen = 4096

// VoiceTranscriber transcribes a downloaded audio file (any format ffmpeg can
// decode) into text. It is defined here so the channel can be wired with a
// concrete speech-to-text implementation only when the feature is enabled,
// while remaining nil (and a no-op) otherwise.
type VoiceTranscriber interface {
	TranscribeFile(ctx context.Context, audioPath string) (string, error)
}

// TelegramChannel implements domain.Channel for the Telegram Bot API
// using the go-telegram/bot SDK with long-polling.
type TelegramChannel struct {
	cfg         config.TelegramChannelConfig
	bot         *bot.Bot
	transcriber VoiceTranscriber
	retention   *VoiceRetention
}

// NewTelegramChannel creates a new Telegram channel. transcriber may be nil, in
// which case inbound voice messages are ignored. retention may be nil to disable
// local persistence of inbound voice/audio files.
func NewTelegramChannel(cfg config.TelegramChannelConfig, transcriber VoiceTranscriber, retention *VoiceRetention) *TelegramChannel {
	return &TelegramChannel{cfg: cfg, transcriber: transcriber, retention: retention}
}

// Name returns the channel identifier
func (t *TelegramChannel) Name() string {
	return "telegram"
}

// Start begins long-polling for Telegram updates and sends inbound messages to the inbox
func (t *TelegramChannel) Start(ctx context.Context, inbox chan<- domain.InboundMessage) error {
	if t.cfg.BotToken == "" {
		return fmt.Errorf("telegram bot token is required")
	}

	b, err := bot.New(t.cfg.BotToken,
		bot.WithDefaultHandler(func(ctx context.Context, b *bot.Bot, update *models.Update) {
			msg := processUpdate(update)
			if msg == nil {
				return
			}

			if update.CallbackQuery != nil {
				_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID,
				})
				if update.CallbackQuery.Message.Message != nil {
					action := strings.SplitN(update.CallbackQuery.Data, ":", 2)[0]
					statusText := "❌ Rejected"
					if action == "approve" {
						statusText = "✅ Approved"
					}
					origMsg := update.CallbackQuery.Message.Message
					_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
						ChatID:    origMsg.Chat.ID,
						MessageID: origMsg.ID,
						Text:      origMsg.Text + "\n\n" + statusText,
					})
				}
			}

			if fileID, ok := msg.Metadata["photo_file_id"]; ok && fileID != "" {
				if img, err := downloadTelegramPhoto(ctx, b, t.cfg.BotToken, fileID); err != nil {
					logger.Error("failed to download photo: %v", err)
				} else {
					msg.Images = append(msg.Images, *img)
				}
			}

			if fileID, ok := msg.Metadata["voice_file_id"]; ok && fileID != "" {
				if !t.applyVoiceTranscription(ctx, b, msg, fileID) {
					return
				}
			}

			select {
			case inbox <- *msg:
			case <-ctx.Done():
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("creating telegram bot: %w", err)
	}

	t.bot = b

	logger.Info("starting long-polling")

	b.Start(ctx)
	return ctx.Err()
}

// Send delivers a message through the Telegram Bot API
func (t *TelegramChannel) Send(ctx context.Context, msg domain.OutboundMessage) error {
	if t.bot == nil {
		return fmt.Errorf("telegram bot not started")
	}

	chatID, err := strconv.ParseInt(msg.RecipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID %q: %w", msg.RecipientID, err)
	}

	chunks := splitMessage(msg.Content, maxMessageLen)
	for _, chunk := range chunks {
		_, err := t.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   chunk,
		})
		if err != nil {
			return fmt.Errorf("sendMessage: %w", err)
		}
	}
	return nil
}

// SendApproval sends a tool approval prompt with inline keyboard buttons.
// Implements domain.ApprovalChannel.
func (t *TelegramChannel) SendApproval(ctx context.Context, recipientID string, req *domain.ApprovalRequest) error {
	if t.bot == nil {
		return fmt.Errorf("telegram bot not started")
	}

	chatID, err := strconv.ParseInt(recipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID %q: %w", recipientID, err)
	}

	text := formatApprovalText(req)

	kb := &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{
				{Text: "✅ Approve", CallbackData: "approve:" + req.ToolCallID},
				{Text: "❌ Reject", CallbackData: "reject:" + req.ToolCallID},
			},
		},
	}

	_, err = t.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        text,
		ReplyMarkup: kb,
	})
	return err
}

// Stop gracefully shuts down the Telegram channel
func (t *TelegramChannel) Stop() error {
	return nil
}

// processUpdate converts a Telegram update into an InboundMessage (or nil if skipped)
func processUpdate(update *models.Update) *domain.InboundMessage {
	if update.CallbackQuery != nil {
		return processCallbackQuery(update.CallbackQuery)
	}

	if update.Message == nil {
		return nil
	}
	msg := update.Message

	if msg.Video != nil {
		logger.Warn("skipping video message from %d", msg.Chat.ID)
		return nil
	}

	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}

	if content == "" && len(msg.Photo) == 0 && msg.Voice == nil && msg.Audio == nil {
		return nil
	}

	senderID := strconv.FormatInt(msg.Chat.ID, 10)

	metadata := map[string]string{
		"message_id": strconv.Itoa(msg.ID),
		"chat_type":  string(msg.Chat.Type),
	}
	if msg.From != nil {
		metadata["username"] = msg.From.Username
		metadata["first_name"] = msg.From.FirstName
		metadata["user_id"] = strconv.FormatInt(msg.From.ID, 10)
	}

	inbound := &domain.InboundMessage{
		ChannelName: "telegram",
		SenderID:    senderID,
		Content:     content,
		Timestamp:   time.Unix(int64(msg.Date), 0),
		Metadata:    metadata,
	}

	if len(msg.Photo) > 0 {
		largest := msg.Photo[len(msg.Photo)-1]
		inbound.Metadata["photo_file_id"] = largest.FileID
		inbound.Metadata["photo_width"] = strconv.Itoa(largest.Width)
		inbound.Metadata["photo_height"] = strconv.Itoa(largest.Height)

		if inbound.Content == "" {
			inbound.Content = "[Attached image]"
		}
	}

	if msg.Voice != nil {
		inbound.Metadata["voice_file_id"] = msg.Voice.FileID
		if inbound.Content == "" {
			inbound.Content = "[Voice message]"
		}
	} else if msg.Audio != nil {
		inbound.Metadata["voice_file_id"] = msg.Audio.FileID
		if inbound.Content == "" {
			inbound.Content = "[Audio message]"
		}
	}

	return inbound
}

// applyVoiceTranscription downloads the voice/audio file referenced by fileID,
// transcribes it, and replaces the message content with the transcription. It
// returns false when the message should be dropped (transcription disabled,
// failed, or produced no text), matching the prior behavior of ignoring voice
// messages the bot cannot turn into text.
func (t *TelegramChannel) applyVoiceTranscription(ctx context.Context, b *bot.Bot, msg *domain.InboundMessage, fileID string) bool {
	if t.transcriber == nil {
		logger.Warn("received a voice message but speech-to-text is disabled; ignoring")
		return false
	}

	text, err := t.transcribeVoice(ctx, b, fileID)
	if err != nil {
		logger.Error("failed to transcribe voice message: %v", err)
		return false
	}
	if strings.TrimSpace(text) == "" {
		logger.Warn("voice message transcription was empty; ignoring")
		return false
	}

	switch msg.Content {
	case "", "[Voice message]", "[Audio message]":
		msg.Content = text
	default:
		msg.Content = msg.Content + "\n" + text
	}
	return true
}

// transcribeVoice downloads the referenced Telegram file, optionally retains a
// copy of the original audio, and transcribes it via the configured transcriber.
func (t *TelegramChannel) transcribeVoice(ctx context.Context, b *bot.Bot, fileID string) (string, error) {
	data, filePath, err := fetchTelegramFile(ctx, b, t.cfg.BotToken, fileID)
	if err != nil {
		return "", err
	}

	if t.retention != nil {
		if _, err := t.retention.save(filePath, data); err != nil {
			logger.Warn("failed to retain inbound voice recording", "error", err)
		}
	}

	tmp, err := os.CreateTemp("", "infer-tg-voice-*"+filepath.Ext(filePath))
	if err != nil {
		return "", fmt.Errorf("creating temp audio file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("writing temp audio file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("closing temp audio file: %w", err)
	}

	return t.transcriber.TranscribeFile(ctx, tmpName)
}

// downloadTelegramPhoto fetches a photo from Telegram's file API and returns it as an ImageAttachment.
func downloadTelegramPhoto(ctx context.Context, b *bot.Bot, token, fileID string) (*domain.ImageAttachment, error) {
	data, filePath, err := fetchTelegramFile(ctx, b, token, fileID)
	if err != nil {
		return nil, err
	}

	return &domain.ImageAttachment{
		Data:        base64.StdEncoding.EncodeToString(data),
		MimeType:    mimeFromPath(filePath),
		Filename:    filepath.Base(filePath),
		DisplayName: filepath.Base(filePath),
	}, nil
}

// fetchTelegramFile resolves a file_id to its download path and returns the raw
// file bytes along with the Telegram file path (used for extension/MIME hints).
func fetchTelegramFile(ctx context.Context, b *bot.Bot, token, fileID string) ([]byte, string, error) {
	file, err := b.GetFile(ctx, &bot.GetFileParams{FileID: fileID})
	if err != nil {
		return nil, "", fmt.Errorf("getFile: %w", err)
	}
	if file.FilePath == "" {
		return nil, "", fmt.Errorf("empty file path for file_id %s", fileID)
	}

	url := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", token, file.FilePath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("downloading file: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			logger.Error("failed to close response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading response: %w", err)
	}

	return data, file.FilePath, nil
}

// mimeFromPath guesses MIME type from a file path extension.
func mimeFromPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// processCallbackQuery converts a Telegram callback query (e.g., inline button click)
// into an InboundMessage with approval metadata. Returns nil for unrecognized data.
func processCallbackQuery(cq *models.CallbackQuery) *domain.InboundMessage {
	parts := strings.SplitN(cq.Data, ":", 2)
	if len(parts) != 2 {
		return nil
	}

	action := parts[0]
	toolCallID := parts[1]

	var approved string
	switch action {
	case "approve":
		approved = "true"
	case "reject":
		approved = "false"
	default:
		return nil
	}

	var chatID int64
	if cq.Message.Message != nil {
		chatID = cq.Message.Message.Chat.ID
	} else if cq.Message.InaccessibleMessage != nil {
		chatID = cq.Message.InaccessibleMessage.Chat.ID
	} else {
		return nil
	}

	return &domain.InboundMessage{
		ChannelName: "telegram",
		SenderID:    strconv.FormatInt(chatID, 10),
		Content:     action,
		Timestamp:   time.Now(),
		Metadata: map[string]string{
			"approval_response": "true",
			"approved":          approved,
			"tool_call_id":      toolCallID,
			"callback_query_id": cq.ID,
			"user_id":           strconv.FormatInt(cq.From.ID, 10),
		},
	}
}

// formatApprovalText creates a prompt for inline keyboard approval (no "Reply yes/no" text).
func formatApprovalText(req *domain.ApprovalRequest) string {
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

	return sb.String()
}

// splitMessage splits a long message into chunks that fit Telegram's message limit
func splitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}

		cutPoint := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			cutPoint = idx + 1
		}

		chunks = append(chunks, text[:cutPoint])
		text = text[cutPoint:]
	}

	return chunks
}
