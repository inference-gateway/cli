package conversations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	container "github.com/inference-gateway/cli/internal/container"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	projects "github.com/inference-gateway/cli/internal/projects"
)

func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	command := &cobra.Command{
		Use:   "conversations",
		Short: "Manage conversation history",
		Long: `View and manage saved conversation history from the database.

This command allows you to list, search, analyze, and delete past conversations
stored in your configured storage backend (SQLite, PostgreSQL, or Redis).`,
	}

	listCommand := &cobra.Command{
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return listConversations(state, renderer, cmd)
		},
	}
	listCommand.Flags().IntP("limit", "l", 50, "Maximum number of conversations to display")
	listCommand.Flags().Int("offset", 0, "Number of conversations to skip (for pagination)")
	output.AddFormatFlag(listCommand, "text", "json")
	listCommand.Flags().Bool("all-projects", false, "List conversations across all projects instead of only the current one")

	deleteCommand := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a saved conversation",
		Long: `Delete a saved conversation by its session ID.

Removes the conversation and all its entries from the configured storage
backend. This operation is permanent and cannot be undone.

Runs non-interactively - no confirmation prompt is shown, so the caller
(the desktop app, scripts, etc.) owns the confirmation UX.

Examples:
  # Delete a conversation by session id
  infer conversations delete 12345678-1234-1234-1234-123456789abc

  # Delete by session group name (e.g. a channel group key)
  infer conversations delete channel-telegram-12345`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteConversation(state, cmd.OutOrStdout(), args[0])
		},
	}

	showCommand := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show the entries of a single conversation",
		Long: `Print the entries of a saved conversation in chronological order.

Each entry shows its role, timestamp, content, and tool_call_id (for tool
results). Output works against any configured storage backend (jsonl, sqlite,
postgres, redis, memory) because it loads through the storage layer rather than
reading files directly.

Hidden entries - system reminders, plan-approval prompts, drained background-task
results, and the synthetic verify message injected by 'infer headless' - are omitted
by default. Pass --include-hidden to surface them.

By default only the first 10 entries are printed so a careless show does not flood
its reader. Pass --no-trunc for every entry, or --append (-a) for the entries after
those first 10.

The session id is resolved the same way as 'infer headless --session-id': a literal
UUID is used as-is, while any other value is treated as a session group key and
resolved to that group's current session id (registering the group if it is new).

Examples:
  # Show a conversation by session id
  infer conversations show 12345678-1234-1234-1234-123456789abc

  # Show by session group name (e.g. a channel group key)
  infer conversations show channel-telegram-12345

  # Include hidden entries such as system reminders
  infer conversations show <session-id> --include-hidden

  # Only the entries whose text mentions a substring
  infer conversations show <session-id> --grep "undefined symbol"

  # Show the entries after the default first-10 window
  infer conversations show <session-id> --append

  # Emit one JSON object per line for piping into jq
  infer conversations show <session-id> --format json | jq .`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showConversation(state, cmd, args[0])
		},
	}
	showCommand.Flags().Bool("include-hidden", false, "Include hidden entries (system reminders, plan prompts, drained background results, verify message)")
	output.AddFormatFlag(showCommand, "text", "json")
	showCommand.Flags().String("grep", "", "Only show the entries whose text contains this substring (case-insensitive)")
	showCommand.Flags().Bool("no-trunc", false, "Print every entry instead of the default first-10 window")
	showCommand.Flags().BoolP("append", "a", false, "Print the entries after the default first-10 window instead of the window itself")

	command.AddCommand(listCommand, deleteCommand, showCommand)
	return command
}

func listConversations(state *runtime.State, renderer *output.Renderer, cmd *cobra.Command) error {
	services := container.NewServiceContainer(state.Config())

	store := services.GetStorage()
	if store == nil {
		return fmt.Errorf("storage is not configured")
	}

	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	format, _ := cmd.Flags().GetString("format")

	scope := ""
	if allProjects, _ := cmd.Flags().GetBool("all-projects"); !allProjects {
		scope = projects.Path()
	}

	ctx := context.Background()
	conversations, err := store.ListConversations(ctx, scope, limit, offset)
	if err != nil {
		return fmt.Errorf("failed to list conversations: %w", err)
	}

	if format == "json" {
		return renderConversationsJSON(cmd.OutOrStdout(), conversations)
	}

	return renderConversationsTable(cmd.OutOrStdout(), renderer, conversations, limit, offset)
}

func renderConversationsJSON(w io.Writer, conversations []convdomain.ConversationSummary) error {
	for i := range conversations {
		if conversations[i].InvokedBy == "" {
			conversations[i].InvokedBy = "human"
		}
	}
	payload := struct {
		Conversations []convdomain.ConversationSummary `json:"conversations"`
		Count         int                              `json:"count"`
	}{
		Conversations: conversations,
		Count:         len(conversations),
	}
	return output.PrintJSON(w, payload)
}

func renderConversationsTable(w io.Writer, renderer *output.Renderer, conversations []convdomain.ConversationSummary, limit, offset int) error {
	if len(conversations) == 0 {
		fmt.Fprintln(w, "No conversations found.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Start a new conversation with: infer chat")
		return nil
	}

	fmt.Fprintln(w, renderer.Title(fmt.Sprintf("Saved Conversations (%d)", len(conversations))))
	fmt.Fprintln(w)

	t := renderer.NewListTable("ID", "Summary", "Messages", "Requests", "Input", "Output", "Cost")
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
	fmt.Fprintln(w, t.Render())

	switch {
	case len(conversations) >= limit:
		fmt.Fprintln(w)
		fmt.Fprintln(w, renderer.Hint(fmt.Sprintf("Showing %d-%d (use --limit and --offset for pagination)",
			offset+1, offset+len(conversations))))
	case offset > 0:
		fmt.Fprintln(w)
		fmt.Fprintln(w, renderer.Hint(fmt.Sprintf("Showing %d-%d", offset+1, offset+len(conversations))))
	}

	return nil
}

func deleteConversation(state *runtime.State, w io.Writer, rawID string) error {
	services := container.NewServiceContainer(state.Config())

	store := services.GetStorage()
	if store == nil {
		return fmt.Errorf("storage is not configured")
	}

	sessionID := resolveConversationSessionID(services, rawID)

	ctx := context.Background()
	if err := store.DeleteConversation(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to delete conversation: %w", err)
	}

	fmt.Fprintf(w, "Deleted conversation: %s\n", sessionID)
	return nil
}

func showConversation(state *runtime.State, cmd *cobra.Command, rawID string) error {
	services := container.NewServiceContainer(state.Config())

	store := services.GetStorage()
	if store == nil {
		return fmt.Errorf("storage is not configured")
	}

	includeHidden, _ := cmd.Flags().GetBool("include-hidden")
	format, _ := cmd.Flags().GetString("format")
	grep, _ := cmd.Flags().GetString("grep")
	noTrunc, _ := cmd.Flags().GetBool("no-trunc")
	showRest, _ := cmd.Flags().GetBool("append")

	sessionID := resolveConversationSessionID(services, rawID)

	ctx := context.Background()
	entries, metadata, err := store.LoadConversation(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("failed to load conversation: %w", err)
	}

	entries = filterConversationEntries(entries, includeHidden)
	entries = grepConversationEntries(entries, grep)
	window, start, total := windowConversationEntries(entries, noTrunc, showRest)

	if format == "json" {
		return printConversationShowJSON(cmd.OutOrStdout(), window, metadata, start, total)
	}
	return printConversationShowText(cmd.OutOrStdout(), window, sessionID, start, total)
}

// resolveConversationSessionID mirrors 'infer headless --session-id' resolution:
// a literal UUID is passed through unchanged, while any other value is resolved
// via the rollover manager's session-group lookup. Falls back to the raw id when
// no rollover manager is configured.
func resolveConversationSessionID(services *container.ServiceContainer, rawID string) string {
	mgr := services.GetSessionRollover()
	if mgr == nil {
		return rawID
	}
	if resolved, _, _ := mgr.ResolveSessionID(rawID); resolved != "" {
		return resolved
	}
	return rawID
}

// filterConversationEntries drops entries marked Hidden unless includeHidden is set.
func filterConversationEntries(entries []convdomain.ConversationEntry, includeHidden bool) []convdomain.ConversationEntry {
	if includeHidden {
		return entries
	}
	filtered := make([]convdomain.ConversationEntry, 0, len(entries))
	for _, e := range entries {
		if e.Hidden {
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered
}

// grepConversationEntries keeps only the entries whose text content contains
// the pattern (case-insensitive). An empty pattern keeps everything.
func grepConversationEntries(entries []convdomain.ConversationEntry, pattern string) []convdomain.ConversationEntry {
	if pattern == "" {
		return entries
	}
	needle := strings.ToLower(pattern)
	filtered := make([]convdomain.ConversationEntry, 0, len(entries))
	for _, e := range entries {
		text := strings.ToLower(formatting.ExtractTextFromContent(e.Message.Content, e.Images))
		if strings.Contains(text, needle) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// defaultShowEntries caps 'conversations show' unless --no-trunc lifts it.
const defaultShowEntries = 10

// windowConversationEntries slices the filtered entries down to the default first
// window: --no-trunc keeps everything, --append (-a) keeps only the entries after
// the window so a session can be read in chunks, and a short session is never
// windowed. start is the 1-based number of the first returned entry, total is what
// the filters left.
func windowConversationEntries(entries []convdomain.ConversationEntry, noTrunc, showRest bool) ([]convdomain.ConversationEntry, int, int) {
	total := len(entries)
	if noTrunc || total <= defaultShowEntries {
		return entries, 1, total
	}
	if showRest {
		return entries[defaultShowEntries:], defaultShowEntries + 1, total
	}
	return entries[:defaultShowEntries], 1, total
}

// buildConversationShowText renders a window of entries as a human-friendly
// plain-text block, numbering each one from start so the #N handles stay true to
// where the entry sits in the session. It is a pure function: it returns the
// rendered string and prints nothing.
func buildConversationShowText(entries []convdomain.ConversationEntry, sessionID string, start, total int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Conversation: %s\n", sessionID)
	fmt.Fprintf(&b, "Entries: %d\n\n", len(entries))

	if len(entries) == 0 {
		b.WriteString("(no entries)\n")
		return b.String()
	}

	for i, e := range entries {
		b.WriteString(buildConversationEntryHeader(start+i-1, e))
		b.WriteString(formatting.ExtractTextFromContent(e.Message.Content, e.Images))
		b.WriteString("\n\n")
	}

	switch {
	case start > 1:
		fmt.Fprintf(&b, "Skipped the first %d entries - rerun with --no-trunc to see them and everything else.\n", start-1)
	case start+len(entries)-1 < total:
		fmt.Fprintf(&b, "Showing the first %d of %d entries - rerun with --append (-a) for the rest or with --no-trunc for everything.\n", len(entries), total)
	}
	return b.String()
}

// buildConversationEntryHeader builds the single header line for one entry, e.g.
// "#1 [user] 2026-05-29T10:00:00Z [hidden] [tool_call_id=call_x] [model=gpt-4o]".
func buildConversationEntryHeader(index int, e convdomain.ConversationEntry) string {
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
	Role             string                           `json:"role"`
	Time             string                           `json:"time"`
	Content          string                           `json:"content"`
	ToolCallID       string                           `json:"tool_call_id,omitempty"`
	Hidden           bool                             `json:"hidden,omitempty"`
	Model            string                           `json:"model,omitempty"`
	ToolExecution    *agentdomain.ToolExecutionResult `json:"tool_execution,omitempty"`
	ReasoningContent string                           `json:"reasoning_content,omitempty"`
}

func toConversationShowEntry(e convdomain.ConversationEntry) conversationShowEntry {
	out := conversationShowEntry{
		Role:             string(e.Message.Role),
		Time:             e.Time.Format(time.RFC3339),
		Content:          formatting.ExtractTextFromContent(e.Message.Content, e.Images),
		Hidden:           e.Hidden,
		Model:            e.Model,
		ToolExecution:    e.ToolExecution,
		ReasoningContent: e.ReasoningContent,
	}
	if e.Message.ToolCallID != nil {
		out.ToolCallID = *e.Message.ToolCallID
	}
	return out
}

// showConversationOutput is the JSON structure for 'conversations show --format json'.
type showConversationOutput struct {
	Metadata  metadataProjection      `json:"metadata"`
	Entries   []conversationShowEntry `json:"entries"`
	Truncated bool                    `json:"truncated,omitempty"`
}

// metadataProjection exposes the metadata fields consumers need, including
// parent_session_id and invoked_by.
type metadataProjection struct {
	ID              string                       `json:"id"`
	Title           string                       `json:"title"`
	CreatedAt       string                       `json:"created_at"`
	UpdatedAt       string                       `json:"updated_at"`
	MessageCount    int                          `json:"message_count"`
	ParentSessionID string                       `json:"parent_session_id"`
	InvokedBy       string                       `json:"invoked_by"`
	TokenStats      convdomain.SessionTokenStats `json:"token_stats"`
	TotalCost       float64                      `json:"total_cost"`
}

func toMetadataProjection(m convdomain.ConversationMetadata) metadataProjection {
	invokedBy := m.InvokedBy
	if invokedBy == "" {
		invokedBy = "human"
	}
	return metadataProjection{
		ID:              m.ID,
		Title:           m.Title,
		CreatedAt:       m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       m.UpdatedAt.Format(time.RFC3339),
		MessageCount:    m.MessageCount,
		ParentSessionID: m.ParentSessionID,
		InvokedBy:       invokedBy,
		TokenStats:      m.TokenStats,
		TotalCost:       m.CostStats.TotalCost,
	}
}

// buildConversationShowJSON returns the JSON representation of a conversation
// including metadata and entries; truncated flags a windowed view.
func buildConversationShowJSON(entries []convdomain.ConversationEntry, metadata convdomain.ConversationMetadata, truncated bool) (string, error) {
	entryProjections := make([]conversationShowEntry, 0, len(entries))
	for _, e := range entries {
		entryProjections = append(entryProjections, toConversationShowEntry(e))
	}
	out := showConversationOutput{
		Metadata:  toMetadataProjection(metadata),
		Entries:   entryProjections,
		Truncated: truncated,
	}
	jsonBytes, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal conversation show output: %w", err)
	}
	return string(jsonBytes), nil
}

func printConversationShowText(w io.Writer, entries []convdomain.ConversationEntry, sessionID string, start, total int) error {
	fmt.Fprint(w, buildConversationShowText(entries, sessionID, start, total))
	return nil
}

func printConversationShowJSON(w io.Writer, entries []convdomain.ConversationEntry, metadata convdomain.ConversationMetadata, start, total int) error {
	out, err := buildConversationShowJSON(entries, metadata, start > 1 || total > len(entries))
	if err != nil {
		return err
	}
	_, werr := fmt.Fprintln(w, out)
	return werr
}
