package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cobra "github.com/spf13/cobra"

	container "github.com/inference-gateway/cli/internal/container"
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

func init() {
	conversationsCmd.AddCommand(conversationsListCmd)

	conversationsListCmd.Flags().IntP("limit", "l", 50, "Maximum number of conversations to display")
	conversationsListCmd.Flags().Int("offset", 0, "Number of conversations to skip (for pagination)")
	conversationsListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")

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

	var md strings.Builder
	fmt.Fprintf(&md, "**SAVED CONVERSATIONS:** %d total\n\n", len(conversations))

	md.WriteString("| ID                                   | Summary                  | Messages | Requests | Input Tokens | Output Tokens | Cost    |\n")
	md.WriteString("|--------------------------------------|--------------------------|----------|----------|--------------|---------------|---------|" + "\n")

	for _, conv := range conversations {
		id := conv.ID
		summary := formatting.TruncateText(conv.Title, 25)
		messages := fmt.Sprintf("%d", conv.MessageCount)
		requests := fmt.Sprintf("%d", conv.TokenStats.RequestCount)
		inputTokens := fmt.Sprintf("%d", conv.TokenStats.TotalInputTokens)
		outputTokens := fmt.Sprintf("%d", conv.TokenStats.TotalOutputTokens)
		cost := formatting.FormatCost(conv.CostStats.TotalCost)

		fmt.Fprintf(&md, "| %-36s | %-24s | %8s | %8s | %12s | %13s | %7s |\n",
			id, summary, messages, requests, inputTokens, outputTokens, cost)
	}

	if len(conversations) >= limit {
		fmt.Fprintf(&md, "\nShowing %d-%d conversations (use --limit and --offset for pagination)\n",
			offset+1, offset+len(conversations))
	} else if offset > 0 {
		fmt.Fprintf(&md, "\nShowing %d-%d of conversations\n",
			offset+1, offset+len(conversations))
	}

	rendered, err := renderMarkdown(md.String())
	if err != nil {
		fmt.Print(md.String())
		return nil
	}

	fmt.Print(rendered)
	return nil
}
