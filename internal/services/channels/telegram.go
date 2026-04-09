package channels

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	bot "github.com/go-telegram/bot"
	models "github.com/go-telegram/bot/models"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

const maxMessageLen = 4096

// TelegramChannel implements domain.Channel for the Telegram Bot API
// using the go-telegram/bot SDK with long-polling.
type TelegramChannel struct {
	cfg config.TelegramChannelConfig
	bot *bot.Bot
}

// NewTelegramChannel creates a new Telegram channel
func NewTelegramChannel(cfg config.TelegramChannelConfig) *TelegramChannel {
	return &TelegramChannel{cfg: cfg}
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
			if msg != nil {
				select {
				case inbox <- *msg:
				case <-ctx.Done():
				}
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("creating telegram bot: %w", err)
	}

	t.bot = b

	log.Printf("[telegram] starting long-polling")

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

// Stop gracefully shuts down the Telegram channel
func (t *TelegramChannel) Stop() error {
	return nil
}

// processUpdate converts a Telegram update into an InboundMessage (or nil if skipped)
func processUpdate(update *models.Update) *domain.InboundMessage {
	if update.Message == nil {
		return nil
	}
	msg := update.Message

	if msg.Video != nil {
		log.Printf("[telegram] skipping video message from %d", msg.Chat.ID)
		return nil
	}

	content := msg.Text
	if content == "" && msg.Caption != "" {
		content = msg.Caption
	}

	if content == "" && len(msg.Photo) == 0 {
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
	}

	return inbound
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
