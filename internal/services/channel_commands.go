package services

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	uuid "github.com/google/uuid"

	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	logger "github.com/inference-gateway/cli/internal/logger"
	shortcuts "github.com/inference-gateway/cli/internal/shortcuts"
	sdk "github.com/inference-gateway/sdk"
)

// ChannelBuiltinCommands are the shortcuts the daemon implements natively for
// channels (everything else in the registry is either a custom shortcut,
// executed as-is, or TUI-only and answered with a pointer to `infer chat`).
var ChannelBuiltinCommands = []domain.ChannelCommand{
	{Name: "new", Description: "Start a fresh conversation and wipe recent chat messages (previous one is kept)"},
	{Name: "conversations", Description: "List past conversations, tap one to switch"},
	{Name: "stats", Description: "Usage stats for this conversation — phone-friendly list (/stats 24h; add 'table' for tables)"},
	{Name: "help", Description: "List available commands"},
}

// maxListedConversations caps the /conversations button list.
const maxListedConversations = 10

// SetCommandSupport enables slash-command handling for inbound channel
// messages. A nil registry leaves the feature disabled.
func (cm *ChannelManagerService) SetCommandSupport(reg *shortcuts.Registry, conv storage.ConversationStorage, groups storage.SessionGroupStorage) {
	cm.shortcutRegistry = reg
	cm.convStore = conv
	cm.groupStore = groups
}

// parseChannelCommand reports whether content is a registered slash command.
// Unregistered "/..." text (e.g. a bare file path) falls through to the agent.
func (cm *ChannelManagerService) parseChannelCommand(content string) (string, []string, bool) {
	if cm.shortcutRegistry == nil || !strings.HasPrefix(strings.TrimSpace(content), "/") {
		return "", nil, false
	}
	name, args, err := cm.shortcutRegistry.ParseShortcut(strings.TrimSpace(content))
	if err != nil {
		return "", nil, false
	}
	name, _, _ = strings.Cut(name, "@") // Telegram group form: /clear@MyBot
	name = strings.ToLower(name)
	if _, exists := cm.shortcutRegistry.Get(name); !exists {
		return "", nil, false
	}
	return name, args, true
}

// handleCommand executes a slash command for a sender. It takes the per-sender
// mutex so destructive commands queue behind any in-flight agent run.
func (cm *ChannelManagerService) handleCommand(ctx context.Context, msg domain.InboundMessage, name string, args []string) {
	senderKey := fmt.Sprintf("%s-%s", msg.ChannelName, msg.SenderID)
	mu := cm.getSenderMutex(senderKey)
	mu.Lock()
	defer mu.Unlock()

	cm.mu.RLock()
	ch, exists := cm.channels[msg.ChannelName]
	cm.mu.RUnlock()
	if !exists {
		logger.Error("channel not found for command routing", "channel", msg.ChannelName)
		return
	}

	replyWith := func(content string, buttons []domain.MessageButton) {
		out := domain.OutboundMessage{
			ChannelName: msg.ChannelName,
			RecipientID: msg.SenderID,
			Content:     content,
			Buttons:     buttons,
			Timestamp:   time.Now(),
		}
		if err := ch.Send(ctx, out); err != nil {
			logger.Error("failed to send command reply", "channel", msg.ChannelName, "error", err)
		}
	}
	reply := func(content string) { replyWith(content, nil) }

	logger.Info("handling channel command", "command", name, "channel", msg.ChannelName, "sender_id", msg.SenderID)
	groupKey := domain.FormatChannelSessionID(msg.ChannelName, msg.SenderID)

	switch name {
	case "new":
		if err := cm.newSession(ctx, groupKey); err != nil {
			reply(fmt.Sprintf("Failed to start a new conversation: %v", err))
			return
		}
		if hc, ok := ch.(domain.HistoryCleaner); ok {
			if err := hc.ClearHistory(ctx, msg.SenderID); err != nil {
				logger.Warn("chat history wipe failed", "channel", msg.ChannelName, "error", err)
			}
		}
		reply("Started a fresh conversation — recent messages cleared. The previous one is kept (see /conversations).")
	case "conversations":
		switch len(args) {
		case 0:
			content, buttons := cm.listConversations(ctx, groupKey)
			replyWith(content, buttons)
		case 1:
			reply(cm.switchConversation(ctx, groupKey, args[0]))
		default:
			reply("Usage: /conversations — or tap a conversation in the list.")
		}
	case "stats":
		groupKey := domain.FormatChannelSessionID(msg.ChannelName, msg.SenderID)
		entry, _, gErr := cm.groupStore.GetSessionGroup(ctx, groupKey)
		if gErr != nil || entry.CurrentSessionID == "" {
			reply("No usage recorded for this conversation yet.")
			return
		}
		since, vertical := shortcuts.ParseStatsArgs(args, true)
		execArgs := make([]string, 0, 2)
		if since != "" {
			execArgs = append(execArgs, since)
		}
		if vertical {
			execArgs = append(execArgs, "vertical")
		}
		res, err := shortcuts.NewStatsShortcut().WithConversation(entry.CurrentSessionID).Execute(ctx, execArgs)
		if err != nil {
			reply(fmt.Sprintf("/stats failed: %v", err))
			return
		}
		replyWith(strings.TrimSpace(res.Output), statsButtons(since, vertical))
	case "help":
		reply(cm.buildCommandHelp())
	case "clear":
		reply("Use /new to start fresh — it also wipes the recent chat messages.")
	default:
		sc, _ := cm.shortcutRegistry.Get(name)
		if _, isCustom := sc.(*shortcuts.CustomShortcut); !isCustom {
			reply(fmt.Sprintf("/%s is only available in the interactive TUI (infer chat): %s", name, sc.GetDescription()))
			return
		}
		res, err := cm.shortcutRegistry.Execute(ctx, name, args)
		if err != nil {
			reply(fmt.Sprintf("/%s failed: %v", name, err))
			return
		}
		out := strings.TrimSpace(res.Output)
		if res.SideEffect != shortcuts.SideEffectNone && res.SideEffect != shortcuts.SideEffectEmbedImages {
			out = strings.TrimSpace(out + "\n\n(The interactive part of this shortcut is not supported in this channel.)")
		}
		if out == "" {
			out = fmt.Sprintf("/%s finished with no output.", name)
		}
		reply(out)
	}
}

// statsButtons builds the window + view-toggle buttons for a /stats reply. Each
// button's callback data is a /stats command tapped back in, changing only one
// dimension: the window buttons keep the current view, the toggle keeps the
// current window. Vertical is the channel default, so only tables carry a token.
func statsButtons(since string, vertical bool) []domain.MessageButton {
	win := ""
	if since != "" {
		win = " " + since
	}
	mode := ""
	if !vertical {
		mode = " table"
	}
	toggle := domain.MessageButton{Text: "Table view", Data: strings.TrimSpace("/stats" + win + " table")}
	if !vertical {
		toggle = domain.MessageButton{Text: "Vertical view", Data: strings.TrimSpace("/stats" + win)}
	}
	return []domain.MessageButton{
		{Text: "Last 24h", Data: strings.TrimSpace("/stats 24h" + mode)},
		{Text: "Last 7d", Data: strings.TrimSpace("/stats 7d" + mode)},
		toggle,
	}
}

// senderConversationIDs returns the sender's conversation IDs, current first,
// then history newest-first. The set is scoped to the sender's session group so
// one Telegram user can never see or switch to another sender's conversations.
func (cm *ChannelManagerService) senderConversationIDs(entry storage.SessionGroupEntry) []string {
	ids := make([]string, 0, len(entry.History)+1)
	if entry.CurrentSessionID != "" {
		ids = append(ids, entry.CurrentSessionID)
	}
	for i := len(entry.History) - 1; i >= 0; i-- {
		if entry.History[i] != "" && entry.History[i] != entry.CurrentSessionID {
			ids = append(ids, entry.History[i])
		}
	}
	return ids
}

// listConversations builds the tap-to-switch conversation list for a sender.
func (cm *ChannelManagerService) listConversations(ctx context.Context, groupKey string) (string, []domain.MessageButton) {
	entry, _, err := cm.groupStore.GetSessionGroup(ctx, groupKey)
	if err != nil {
		return fmt.Sprintf("Failed to list conversations: %v", err), nil
	}

	ids := cm.senderConversationIDs(entry)
	var buttons []domain.MessageButton
	truncated := false
	for _, id := range ids {
		if len(buttons) == maxListedConversations {
			truncated = true
			break
		}
		_, meta, err := cm.convStore.LoadConversation(ctx, id)
		if err != nil {
			continue // never saved (fresh session) or removed server-side
		}
		title := meta.Title
		if title == "" {
			title = "Untitled"
		}
		label := fmt.Sprintf("%s · %d msgs", title, meta.MessageCount)
		if id == entry.CurrentSessionID {
			label = "▸ " + label
		}
		buttons = append(buttons, domain.MessageButton{Text: label, Data: "/conversations " + id})
	}

	if len(buttons) == 0 {
		return "No conversations yet.", nil
	}
	content := "Tap a conversation to switch:"
	if truncated {
		content += fmt.Sprintf(" (showing the %d most recent)", maxListedConversations)
	}
	return content, buttons
}

// switchConversation repoints the sender's session group at an existing
// conversation, matched by full ID (button tap) or unique prefix (typed).
func (cm *ChannelManagerService) switchConversation(ctx context.Context, groupKey, arg string) string {
	entry, _, err := cm.groupStore.GetSessionGroup(ctx, groupKey)
	if err != nil {
		return fmt.Sprintf("Failed to switch conversation: %v", err)
	}

	var matches []string
	for _, id := range cm.senderConversationIDs(entry) {
		if strings.HasPrefix(id, arg) {
			matches = append(matches, id)
		}
	}
	switch {
	case len(matches) == 0:
		return fmt.Sprintf("No conversation matches %q — see /conversations.", arg)
	case len(matches) > 1:
		return fmt.Sprintf("%q is ambiguous — tap one in /conversations instead.", arg)
	}
	target := matches[0]
	if target == entry.CurrentSessionID {
		return "Already on this conversation."
	}

	entries, meta, err := cm.convStore.LoadConversation(ctx, target)
	if err != nil {
		return "That conversation no longer exists — see /conversations."
	}

	history := make([]string, 0, len(entry.History)+1)
	for _, id := range append(entry.History, entry.CurrentSessionID) {
		if id != "" && id != target && !slices.Contains(history, id) {
			history = append(history, id)
		}
	}
	if err := cm.groupStore.PutSessionGroup(ctx, groupKey, storage.SessionGroupEntry{
		CurrentSessionID: target,
		History:          history,
		LastRollover:     entry.LastRollover,
		UpdatedAt:        time.Now(),
	}); err != nil {
		return fmt.Sprintf("Failed to switch conversation: %v", err)
	}

	title := meta.Title
	if title == "" {
		title = "Untitled"
	}
	reply := "Switched to: " + title
	if recap := lastExchangeRecap(entries); recap != "" {
		reply += "\n\nWhere you left off:\n" + recap
	}
	return reply
}

// recapSnippetLen bounds each recap line so the confirmation stays scannable.
const recapSnippetLen = 200

// lastExchangeRecap renders the tail of a conversation (last user message and
// the assistant reply after it) so a switched chat shows its context.
func lastExchangeRecap(entries []domain.ConversationEntry) string {
	var user, assistant string
	for _, e := range entries {
		if e.Hidden || e.ToolExecution != nil {
			continue
		}
		content, _ := e.Message.Content.AsMessageContent0()
		if strings.TrimSpace(content) == "" {
			continue
		}
		switch e.Message.Role {
		case sdk.User:
			user, assistant = content, ""
		case sdk.Assistant:
			assistant = content
		}
	}

	var sb strings.Builder
	if user != "" {
		sb.WriteString("You: " + truncateSnippet(user) + "\n")
	}
	if assistant != "" {
		sb.WriteString("Agent: " + truncateSnippet(assistant))
	}
	return strings.TrimSpace(sb.String())
}

func truncateSnippet(s string) string {
	s = strings.Join(strings.Fields(s), " ") // collapse newlines for a compact recap
	if r := []rune(s); len(r) > recapSnippetLen {
		return string(r[:recapSnippetLen-1]) + "…"
	}
	return s
}

// newSession points the sender's session group at a fresh session ID, keeping
// the previous conversation and recording it in the group history.
func (cm *ChannelManagerService) newSession(ctx context.Context, groupKey string) error {
	entry, found, err := cm.groupStore.GetSessionGroup(ctx, groupKey)
	if err != nil {
		return fmt.Errorf("looking up session group: %w", err)
	}
	history := entry.History
	if found && entry.CurrentSessionID != "" {
		history = append(history, entry.CurrentSessionID)
	}

	if err := cm.groupStore.PutSessionGroup(ctx, groupKey, storage.SessionGroupEntry{
		CurrentSessionID: uuid.NewString(),
		History:          history,
		UpdatedAt:        time.Now(),
	}); err != nil {
		return fmt.Errorf("advancing session group: %w", err)
	}
	return nil
}

// buildCommandHelp composes the /help reply from the shortcut registry.
func (cm *ChannelManagerService) buildCommandHelp() string {
	var sb strings.Builder
	sb.WriteString("Available commands:\n")
	for _, c := range ChannelBuiltinCommands {
		fmt.Fprintf(&sb, "/%s — %s\n", c.Name, c.Description)
	}

	var tuiOnly []string
	for _, sc := range cm.shortcutRegistry.GetAll() {
		name := sc.GetName()
		if isChannelBuiltin(name) {
			continue
		}
		if _, isCustom := sc.(*shortcuts.CustomShortcut); isCustom {
			fmt.Fprintf(&sb, "/%s — %s\n", name, sc.GetDescription())
			continue
		}
		tuiOnly = append(tuiOnly, "/"+name)
	}

	if len(tuiOnly) > 0 {
		sb.WriteString("\nTUI-only (run infer chat): ")
		sb.WriteString(strings.Join(tuiOnly, ", "))
	}
	return sb.String()
}

func isChannelBuiltin(name string) bool {
	for _, c := range ChannelBuiltinCommands {
		if c.Name == name {
			return true
		}
	}
	return false
}
