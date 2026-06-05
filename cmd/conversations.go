package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	formatting "github.com/inference-gateway/cli/internal/formatting"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

var conversationsCmd = &cobra.Command{
	Use:   "conversations",
	Short: "Manage conversation history",
	Long: `View and manage saved conversation history from the database.

This command allows you to list, search, and analyze past conversations
stored in your configured storage backend (SQLite, PostgreSQL, or Redis).`,
}

var conversationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved conversations",
	Long: `Display all saved conversations from the database with metadata.

Shows conversation ID, title, message count, request count, token usage,
and cost information in a markdown table format.

Examples:
  # List all conversations (default: 50)
  infer conversations list

  # List with pagination
  infer conversations list --limit 20 --offset 40

  # Output as JSON
  infer conversations list --format json

  # Compact list command
  infer conversations list -l 10`,
	RunE: listConversations,
}

var conversationsShowCmd = &cobra.Command{
	Use:   "show <session-id>",
	Short: "Show the entries of a single conversation",
	Long: `Print the entries of a saved conversation in chronological order.

Each entry shows its role, timestamp, content, and tool_call_id (for tool
results). Output works against any configured storage backend (jsonl, sqlite,
postgres, redis, memory) because it loads through the storage layer rather than
reading files directly.

Hidden entries - system reminders, plan-approval prompts, drained background-task
results, and the synthetic verify message injected by 'infer agent' - are omitted
by default. Pass --include-hidden to surface them.

The session id is resolved the same way as 'infer agent --session-id': a literal
UUID is used as-is, while any other value is treated as a session group key and
resolved to that group's current session id (registering the group if it is new).

Examples:
  # Show a conversation by session id
  infer conversations show 12345678-1234-1234-1234-123456789abc

  # Show by session group name (e.g. a channel group key)
  infer conversations show channel-telegram-12345

  # Include hidden entries such as system reminders
  infer conversations show <session-id> --include-hidden

  # Emit one JSON object per line for piping into jq
  infer conversations show <session-id> --format json | jq .`,
	Args: cobra.ExactArgs(1),
	RunE: showConversation,
}

func init() {
	conversationsCmd.AddCommand(conversationsListCmd)

	conversationsListCmd.Flags().IntP("limit", "l", 50, "Maximum number of conversations to display")
	conversationsListCmd.Flags().Int("offset", 0, "Number of conversations to skip (for pagination)")
	conversationsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	conversationsCmd.AddCommand(conversationsShowCmd)

	conversationsShowCmd.Flags().Bool("include-hidden", false, "Include hidden entries (system reminders, plan prompts, drained background results, verify message)")
	conversationsShowCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

	rootCmd.AddCommand(conversationsCmd)
}

func listConversations(cmd *cobra.Command, args []string) error {
	services := container.NewServiceContainer(Cfg)

	store := services.GetStorage()
	if store == nil {
		return fmt.Errorf("storage is not configured")
	}

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	format, _ := cmd.Flags().GetString("format")

	ctx := context.Background()
	conversations, err := store.ListConversations(ctx, limit, offset)
	if err != nil {
		return fmt.Errorf("failed to list conversations: %w", err)
	}

	if format == "json" {
		return renderConversationsJSON(conversations)
	}

	return renderConversationsTable(conversations, limit, offset)
}

func renderConversationsJSON(conversations []storage.ConversationSummary) error {
	output := struct {
		Conversations []storage.ConversationSummary `json:"conversations"`
		Count         int                           `json:"count"`
	}{
		Conversations: conversations,
		Count:         len(conversations),
	}

	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal conversations to JSON: %w", err)
	}

	fmt.Println(string(jsonBytes))
	return nil
}

func renderConversationsTable(conversations []storage.ConversationSummary, limit, offset int) error {
	if len(conversations) == 0 {
		fmt.Println("No conversations found.")
		fmt.Println()
		fmt.Println("Start a new conversation with: infer chat")
		return nil
	}

	fmt.Println(listTitle(fmt.Sprintf("Saved Conversations (%d)", len(conversations))))
	fmt.Println()

	t := newListTable("ID", "Summary", "Messages", "Requests", "Input", "Output", "Cost")
	for _, conv := range conversations {
		t.Row(
			conv.ID,
			formatting.TruncateText(conv.Title, 25),
			fmt.Sprintf("%d", conv.MessageCount),
			fmt.Sprintf("%d", conv.TokenStats.RequestCount),
			fmt.Sprintf("%d", conv.TokenStats.TotalInputTokens),
			fmt.Sprintf("%d", conv.TokenStats.TotalOutputTokens),
			formatting.FormatCost(conv.CostStats.TotalCost),
		)
	}
	fmt.Println(t.Render())

	switch {
	case len(conversations) >= limit:
		fmt.Println()
		fmt.Println(listHint(fmt.Sprintf("Showing %d-%d (use --limit and --offset for pagination)",
			offset+1, offset+len(conversations))))
	case offset > 0:
		fmt.Println()
		fmt.Println(listHint(fmt.Sprintf("Showing %d-%d", offset+1, offset+len(conversations))))
	}

	return nil
}

func showConversation(cmd *cobra.Command, args []string) error {
	services := container.NewServiceContainer(Cfg)

	store := services.GetStorage()
	if store == nil {
		return fmt.Errorf("storage is not configured")
	}

	includeHidden, _ := cmd.Flags().GetBool("include-hidden")
	format, _ := cmd.Flags().GetString("format")

	sessionID := resolveConversationSessionID(services, args[0])

	ctx := context.Background()
	entries, _, err := store.LoadConversation(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load conversation: %w", err)
	}

	entries = filterConversationEntries(entries, includeHidden)

	if format == "json" {
		return printConversationShowJSON(entries)
	}
	return printConversationShowText(entries, sessionID)
}

// resolveConversationSessionID mirrors 'infer agent --session-id' resolution:
// a literal UUID is passed through unchanged, while any other value is resolved
// via the rollover manager's session-group lookup. Falls back to the raw id when
// no rollover manager is configured.
func resolveConversationSessionID(services *container.ServiceContainer, rawID string) string {
	mgr := services.GetSessionRolloverManager()
	if mgr == nil {
		return rawID
	}
	if resolved, _, _ := mgr.ResolveSessionID(rawID); resolved != "" {
		return resolved
	}
	return rawID
}

// filterConversationEntries drops entries marked Hidden unless includeHidden is set.
func filterConversationEntries(entries []domain.ConversationEntry, includeHidden bool) []domain.ConversationEntry {
	if includeHidden {
		return entries
	}
	filtered := make([]domain.ConversationEntry, 0, len(entries))
	for _, e := range entries {
		if e.Hidden {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// buildConversationShowText renders entries as a human-friendly plain-text block.
// It is a pure function: it returns the rendered string and prints nothing.
func buildConversationShowText(entries []domain.ConversationEntry, sessionID string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Conversation: %s\n", sessionID)
	fmt.Fprintf(&b, "Entries: %d\n\n", len(entries))

	if len(entries) == 0 {
		b.WriteString("(no entries)\n")
		return b.String()
	}

	for i, e := range entries {
		b.WriteString(buildConversationEntryHeader(i, e))
		b.WriteString(formatting.ExtractTextFromContent(e.Message.Content, e.Images))
		b.WriteString("\n\n")
	}
	return b.String()
}

// buildConversationEntryHeader builds the single header line for one entry, e.g.
// "#1 [user] 2026-05-29T10:00:00Z [hidden] [tool_call_id=call_x] [model=gpt-4o]".
func buildConversationEntryHeader(index int, e domain.ConversationEntry) string {
	var h strings.Builder
	fmt.Fprintf(&h, "#%d [%s] %s", index+1, string(e.Message.Role), e.Time.Format(time.RFC3339))
	if e.Hidden {
		h.WriteString(" [hidden]")
	}
	if e.Message.ToolCallID != nil && *e.Message.ToolCallID != "" {
		fmt.Fprintf(&h, " [tool_call_id=%s]", *e.Message.ToolCallID)
	}
	if e.Model != "" {
		fmt.Fprintf(&h, " [model=%s]", e.Model)
	}
	h.WriteString("\n")
	return h.String()
}

// conversationShowEntry is the compact, jq-friendly projection emitted per line by
// 'conversations show --format json'.
type conversationShowEntry struct {
	Role       string `json:"role"`
	Time       string `json:"time"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	Hidden     bool   `json:"hidden,omitempty"`
	Model      string `json:"model,omitempty"`
}

func toConversationShowEntry(e domain.ConversationEntry) conversationShowEntry {
	out := conversationShowEntry{
		Role:    string(e.Message.Role),
		Time:    e.Time.Format(time.RFC3339),
		Content: formatting.ExtractTextFromContent(e.Message.Content, e.Images),
		Hidden:  e.Hidden,
		Model:   e.Model,
	}
	if e.Message.ToolCallID != nil {
		out.ToolCallID = *e.Message.ToolCallID
	}
	return out
}

// buildConversationShowJSON returns newline-joined compact JSON, one object per
// entry (NDJSON), matching the 'infer agent' stdout shape. Pure function.
func buildConversationShowJSON(entries []domain.ConversationEntry) (string, error) {
	var b strings.Builder
	for _, e := range entries {
		line, err := json.Marshal(toConversationShowEntry(e))
		if err != nil {
			return "", fmt.Errorf("failed to marshal conversation entry: %w", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func printConversationShowText(entries []domain.ConversationEntry, sessionID string) error {
	fmt.Print(buildConversationShowText(entries, sessionID))
	return nil
}

func printConversationShowJSON(entries []domain.ConversationEntry) error {
	out, err := buildConversationShowJSON(entries)
	if err != nil {
		return err
	}
	_, werr := os.Stdout.Write([]byte(out))
	return werr
}
