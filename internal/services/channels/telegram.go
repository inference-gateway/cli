package channels

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	bot "github.com/go-telegram/bot"
	models "github.com/go-telegram/bot/models"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// htmlChunkLen leaves headroom below Telegram's 4096-char message limit for
// HTML entities/tags added by renderTelegramHTML.
const htmlChunkLen = 3500

// maxPhotoBytes is Telegram's upload limit for sendPhoto.
const maxPhotoBytes = 10 << 20

// maxTrackedMessages caps the per-chat message-ID buffer used by ClearHistory.
const maxTrackedMessages = 500

// deleteBatchSize is Telegram's limit for deleteMessages.
const deleteBatchSize = 100

// maxCommandDescLen is Telegram's limit for a bot command description.
const maxCommandDescLen = 256

// maxButtonTextLen keeps inline keyboard labels readable on a phone.
const maxButtonTextLen = 48

// botCommandNameRe matches Telegram's allowed bot command names.
var botCommandNameRe = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

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

	// commands to advertise via SetMyCommands on Start.
	commands []domain.ChannelCommand

	// msgIDs tracks recent message IDs per chat so ClearHistory can delete them.
	// ponytail: in-memory + capped; Telegram can't delete >48h-old messages anyway, persistence buys nothing
	msgMu  sync.Mutex
	msgIDs map[int64][]int
}

// NewTelegramChannel creates a new Telegram channel. transcriber may be nil, in
// which case inbound voice messages are ignored. retention may be nil to disable
// local persistence of inbound voice/audio files.
func NewTelegramChannel(cfg config.TelegramChannelConfig, transcriber VoiceTranscriber, retention *VoiceRetention) *TelegramChannel {
	return &TelegramChannel{cfg: cfg, transcriber: transcriber, retention: retention, msgIDs: make(map[int64][]int)}
}

// SetCommands sets the slash commands advertised to Telegram on Start.
func (t *TelegramChannel) SetCommands(cmds []domain.ChannelCommand) {
	t.commands = cmds
}

// trackMessage remembers a message ID for later deletion by ClearHistory.
func (t *TelegramChannel) trackMessage(chatID int64, messageID int) {
	t.msgMu.Lock()
	defer t.msgMu.Unlock()
	ids := append(t.msgIDs[chatID], messageID)
	if len(ids) > maxTrackedMessages {
		ids = ids[len(ids)-maxTrackedMessages:]
	}
	t.msgIDs[chatID] = ids
}

// ClearHistory best-effort deletes the tracked messages of a chat.
// Implements domain.HistoryCleaner. Telegram silently skips messages it can no
// longer delete (older than 48h), and batches that fail are logged and skipped.
func (t *TelegramChannel) ClearHistory(ctx context.Context, recipientID string) error {
	if t.bot == nil {
		return fmt.Errorf("telegram bot not started")
	}
	chatID, err := strconv.ParseInt(recipientID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID %q: %w", recipientID, err)
	}

	t.msgMu.Lock()
	ids := t.msgIDs[chatID]
	delete(t.msgIDs, chatID)
	t.msgMu.Unlock()

	for start := 0; start < len(ids); start += deleteBatchSize {
		batch := ids[start:min(start+deleteBatchSize, len(ids))]
		if _, err := t.bot.DeleteMessages(ctx, &bot.DeleteMessagesParams{
			ChatID:     chatID,
			MessageIDs: batch,
		}); err != nil {
			logger.Warn("deleteMessages batch failed", "chat_id", chatID, "count", len(batch), "error", err)
		}
	}
	return nil
}

// telegramBotCommands converts channel commands to Telegram bot commands,
// dropping names Telegram would reject and truncating long descriptions.
func telegramBotCommands(cmds []domain.ChannelCommand) []models.BotCommand {
	var out []models.BotCommand
	for _, c := range cmds {
		if !botCommandNameRe.MatchString(c.Name) {
			continue
		}
		desc := c.Description
		if len(desc) > maxCommandDescLen {
			desc = desc[:maxCommandDescLen]
		}
		out = append(out, models.BotCommand{Command: c.Name, Description: desc})
	}
	return out
}

// inlineKeyboard renders message buttons as a one-button-per-row inline
// keyboard (full-width, thumb-friendly). Returns nil for no buttons so callers
// can pass it straight to sendText.
func inlineKeyboard(buttons []domain.MessageButton) models.ReplyMarkup {
	if len(buttons) == 0 {
		return nil
	}
	rows := make([][]models.InlineKeyboardButton, 0, len(buttons))
	for _, b := range buttons {
		text := b.Text
		if r := []rune(text); len(r) > maxButtonTextLen {
			text = string(r[:maxButtonTextLen-1]) + "…"
		}
		rows = append(rows, []models.InlineKeyboardButton{{Text: text, CallbackData: b.Data}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
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
			if update.Message != nil {
				t.trackMessage(update.Message.Chat.ID, update.Message.ID)
			}
			msg := processUpdate(update)
			if msg == nil {
				return
			}

			if update.CallbackQuery != nil {
				_, _ = b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
					CallbackQueryID: update.CallbackQuery.ID,
				})
				if origMsg := update.CallbackQuery.Message.Message; origMsg != nil {
					action, _, _ := strings.Cut(update.CallbackQuery.Data, ":")
					switch action {
					case "approve", "reject":
						statusText := "Rejected"
						if action == "approve" {
							statusText = "Approved"
						}
						_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
							ChatID:    origMsg.Chat.ID,
							MessageID: origMsg.ID,
							Text:      origMsg.Text + "\n\n" + statusText,
						})
					default:
						_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
							ChatID:      origMsg.Chat.ID,
							MessageID:   origMsg.ID,
							ReplyMarkup: models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{}},
						})
					}
				}
			}

			if fileID, ok := msg.Metadata["photo_file_id"]; ok && fileID != "" {
				if img, err := downloadTelegramPhoto(ctx, b, t.cfg.BotToken, fileID); err != nil {
					logger.Error("failed to download photo", "error", err)
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

	if cmds := telegramBotCommands(t.commands); len(cmds) > 0 {
		if _, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{Commands: cmds}); err != nil {
			logger.Warn("setMyCommands failed", "error", err)
		}
	}

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

	paths, text := extractImagePaths(msg.Content)

	if strings.TrimSpace(text) != "" {
		chunks := splitMessage(text, htmlChunkLen)
		for i, chunk := range chunks {
			var kb models.ReplyMarkup
			if i == len(chunks)-1 {
				kb = inlineKeyboard(msg.Buttons)
			}
			if err := t.sendText(ctx, chatID, chunk, kb); err != nil {
				return fmt.Errorf("sendMessage: %w", err)
			}
		}
	}

	for _, p := range paths {
		t.sendImageFile(ctx, chatID, p)
	}
	return nil
}

// sendImageFile uploads a local image, preferring an inline photo but falling back
// to a document when Telegram rejects the photo. Full-page screenshots trip
// PHOTO_INVALID_DIMENSIONS (Telegram caps width+height ≤ 10000 and side-ratio ≤ 20:1);
// sendDocument has no dimension limit, so the user still receives the image as a file.
func (t *TelegramChannel) sendImageFile(ctx context.Context, chatID int64, p string) {
	f, err := os.Open(p)
	if err != nil {
		logger.Warn("skipping outbound image", "path", p, "error", err)
		return
	}
	sent, err := t.bot.SendPhoto(ctx, &bot.SendPhotoParams{
		ChatID: chatID,
		Photo:  &models.InputFileUpload{Filename: filepath.Base(p), Data: f},
	})
	_ = f.Close()

	if err != nil {
		logger.Warn("sendPhoto failed, retrying as document", "path", p, "error", err)
		f2, oerr := os.Open(p) // SendPhoto consumed the first reader; reopen for the retry.
		if oerr != nil {
			logger.Warn("skipping outbound image", "path", p, "error", oerr)
			return
		}
		sent, err = t.bot.SendDocument(ctx, &bot.SendDocumentParams{
			ChatID:   chatID,
			Document: &models.InputFileUpload{Filename: filepath.Base(p), Data: f2},
		})
		_ = f2.Close()
		if err != nil {
			logger.Warn("sendDocument fallback failed", "path", p, "error", err)
			return
		}
	}

	if sent != nil {
		t.trackMessage(chatID, sent.ID)
	}
}

// sendText sends one chunk rendered as Telegram HTML, retrying once as plain
// text only when Telegram rejects the markup (a 400 Bad Request: malformed HTML
// fails the whole message and nothing is delivered). We must NOT retry on other
// errors (network timeout, 429, response-decode): those may have already
// delivered the message, so a blind resend shows the user a duplicate.
func (t *TelegramChannel) sendText(ctx context.Context, chatID int64, text string, kb models.ReplyMarkup) error {
	sent, err := t.bot.SendMessage(ctx, &bot.SendMessageParams{
		ChatID:      chatID,
		Text:        renderTelegramHTML(text),
		ParseMode:   models.ParseModeHTML,
		ReplyMarkup: kb,
	})
	if err != nil && errors.Is(err, bot.ErrorBadRequest) {
		sent, err = t.bot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID:      chatID,
			Text:        text,
			ReplyMarkup: kb,
		})
	}
	if err == nil && sent != nil {
		t.trackMessage(chatID, sent.ID)
	}
	return err
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
				{Text: "Approve", CallbackData: "approve:" + req.ToolCallID},
				{Text: "Reject", CallbackData: "reject:" + req.ToolCallID},
			},
		},
	}

	return t.sendText(ctx, chatID, text, kb)
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
		logger.Warn("skipping video message", "chat_id", msg.Chat.ID)
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
		logger.Error("failed to transcribe voice message", "error", err)
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
			logger.Error("failed to close response body", "error", err)
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
// into an InboundMessage. Approval buttons carry approval metadata; command
// buttons ("/..." callback data) are delivered as if the user typed the
// command. Returns nil for unrecognized data.
func processCallbackQuery(cq *models.CallbackQuery) *domain.InboundMessage {
	if strings.HasPrefix(cq.Data, "/") {
		chatID := callbackChatID(cq)
		if chatID == 0 {
			return nil
		}
		return &domain.InboundMessage{
			ChannelName: "telegram",
			SenderID:    strconv.FormatInt(chatID, 10),
			Content:     cq.Data,
			Timestamp:   time.Now(),
		}
	}

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

	chatID := callbackChatID(cq)
	if chatID == 0 {
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

// callbackChatID extracts the chat ID a callback query originated from
// (0 when Telegram provides no message context at all).
func callbackChatID(cq *models.CallbackQuery) int64 {
	if cq.Message.Message != nil {
		return cq.Message.Message.Chat.ID
	}
	if cq.Message.InaccessibleMessage != nil {
		return cq.Message.InaccessibleMessage.Chat.ID
	}
	return 0
}

// formatApprovalText creates a prompt for inline keyboard approval (no "Reply yes/no" text).
func formatApprovalText(req *domain.ApprovalRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Approve %s?\n", req.ToolName)

	var args map[string]any
	if err := json.Unmarshal([]byte(req.ToolArgs), &args); err == nil {
		if cmd, ok := args["command"].(string); ok {
			fmt.Fprintf(&sb, "```\n%s\n```", cmd)
		} else if filePath, ok := args["file_path"].(string); ok {
			fmt.Fprintf(&sb, "`%s`", filePath)
		}
	}

	return sb.String()
}

var (
	imgTagRe    = regexp.MustCompile(`(?i)<img[^>]*\bsrc="(?:file://)?(/[^"]+)"[^>]*/?>`)
	mdImgRe     = regexp.MustCompile(`!\[[^\]]*\]\((?:file://)?(/[^)]+)\)`)
	bareImgRe   = regexp.MustCompile(`(?i)(?:^|\s)(/[^\s"'<>` + "`" + `]+\.(?:png|jpe?g|gif|webp))`)
	fenceRe     = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\n?(.*?)```")
	inlCodeRe   = regexp.MustCompile("`([^`\n]+)`")
	boldRe      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	headerRe    = regexp.MustCompile(`(?m)^#{1,6} +(.+)$`)
	tableRe     = regexp.MustCompile(`(?m)^\|.*\|[ \t]*$(?:\n^\|.*\|[ \t]*$)*`)
	tableSepRe  = regexp.MustCompile(`^:?-+:?$`)
	sendableExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}
)

// sendableImage reports whether p is an existing image file Telegram will accept as a photo.
func sendableImage(p string) bool {
	if !sendableExt[strings.ToLower(filepath.Ext(p))] {
		return false
	}
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	if fi.Size() > maxPhotoBytes {
		logger.Warn("outbound image exceeds telegram photo limit, skipping", "path", p, "size", fi.Size())
		return false
	}
	return true
}

// extractImagePaths finds local image file references in agent output —
// <img src="..."> tags, markdown ![..](path), and bare absolute paths — and
// returns the sendable ones. Tag and markdown references are stripped from the
// returned text; bare paths stay visible so the user still sees the location.
//
// Fenced code blocks are skipped: tool results are forwarded as a ``` fence
// (formatAgentMessage) whose JSON embeds the saved artifact path — and the
// WebFetch hint's literal ![image](path) template — so scanning it would send the
// photo a second time on top of the assistant's own ![image](path). The assistant
// is told to place its image line outside any code block, so only unfenced text
// is a real "display this" instruction.
func extractImagePaths(content string) ([]string, string) {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) bool {
		if !sendableImage(p) {
			return false
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
		return true
	}

	extract := func(seg string) string {
		for _, re := range []*regexp.Regexp{imgTagRe, mdImgRe} {
			seg = re.ReplaceAllStringFunc(seg, func(m string) string {
				if add(re.FindStringSubmatch(m)[1]) {
					return ""
				}
				return m
			})
		}
		for _, m := range bareImgRe.FindAllStringSubmatch(seg, -1) {
			add(m[1])
		}
		return seg
	}

	var out strings.Builder
	last := 0
	for _, loc := range fenceRe.FindAllStringIndex(content, -1) {
		out.WriteString(extract(content[last:loc[0]]))
		out.WriteString(content[loc[0]:loc[1]]) // fenced block preserved verbatim
		last = loc[1]
	}
	out.WriteString(extract(content[last:]))
	return paths, out.String()
}

// renderTelegramHTML converts common Markdown (fenced code, inline code, bold,
// headers) to Telegram HTML. Everything else is escaped so the message never
// breaks on user content.
func renderTelegramHTML(md string) string {
	var sb strings.Builder
	last := 0
	for _, loc := range fenceRe.FindAllStringSubmatchIndex(md, -1) {
		sb.WriteString(renderInlineHTML(md[last:loc[0]]))
		sb.WriteString("<pre>")
		sb.WriteString(html.EscapeString(strings.TrimRight(md[loc[2]:loc[3]], "\n")))
		sb.WriteString("</pre>")
		last = loc[1]
	}
	sb.WriteString(renderInlineHTML(md[last:]))
	return sb.String()
}

// renderInlineHTML renders a non-fenced markdown segment: pipe tables become
// aligned <pre> blocks, everything else goes through renderProseHTML.
func renderInlineHTML(s string) string {
	var sb strings.Builder
	last := 0
	for _, loc := range tableRe.FindAllStringIndex(s, -1) {
		sb.WriteString(renderProseHTML(s[last:loc[0]]))
		sb.WriteString(renderTable(s[loc[0]:loc[1]]))
		last = loc[1]
	}
	sb.WriteString(renderProseHTML(s[last:]))
	return sb.String()
}

func renderProseHTML(s string) string {
	s = html.EscapeString(s)
	s = inlCodeRe.ReplaceAllString(s, "<code>$1</code>")
	s = boldRe.ReplaceAllString(s, "<b>$1</b>")
	s = headerRe.ReplaceAllString(s, "<b>$1</b>")
	return s
}

// renderTable turns a markdown pipe table into a column-aligned <pre> block,
// dropping the |---|---| separator row. Escaping happens after alignment so
// entity length doesn't skew the columns.
// ponytail: tabwriter measures rune width — fine for ASCII/stats tables; wide
// CJK could misalign. Swap to a width-aware writer only if that ever shows up.
func renderTable(md string) string {
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for line := range strings.SplitSeq(strings.TrimRight(md, "\n"), "\n") {
		cells := splitTableRow(line)
		if isTableSeparator(cells) {
			continue
		}
		_, _ = fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()
	return "<pre>" + html.EscapeString(strings.TrimRight(buf.String(), "\n")) + "</pre>"
}

// splitTableRow trims the outer pipes and returns the trimmed cells of one row.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	cells := strings.Split(line, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(cells[i])
	}
	return cells
}

// isTableSeparator reports whether every cell is a markdown alignment marker (---, :--, etc.).
func isTableSeparator(cells []string) bool {
	for _, c := range cells {
		if !tableSepRe.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
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
