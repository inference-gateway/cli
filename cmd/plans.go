package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	cobra "github.com/spf13/cobra"

	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

var plansCmd = &cobra.Command{
	Use:   "plans",
	Short: "Manage saved plans",
	Long: `View and manage saved plans from plan-mode sessions.

Plans are persisted to the configured storage backend (sqlite, postgres,
redis, jsonl, memory, or d1) when the agent uses the RequestPlanApproval
tool. Each plan gets an infer://plans/<id> URI that can be used to
retrieve it later.`,
}

var plansShowCmd = &cobra.Command{
	Use:   "show <plan-id>",
	Short: "Show a saved plan by ID",
	Long: `Print the full content of a saved plan.

The plan ID is the "<timestamp>-<slug>" identifier returned by the
RequestPlanApproval tool as part of the infer://plans/<id> URI. Pass the
full URI or just the ID.

Output is rendered as styled markdown on a terminal and printed as raw
markdown when piped, redirected, or run with --no-colors.

Examples:
  # Show by plan ID
  infer plans show 2026-07-17-153000-add-user-auth

  # Show by infer URI
  infer plans show infer://plans/2026-07-17-153000-add-user-auth`,
	Args: cobra.ExactArgs(1),
	RunE: showPlan,
}

var plansListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all saved plans",
	Long: `Display all saved plans with their title and creation time.

Examples:
  # List all plans
  infer plans list

  # Output as JSON
  infer plans list --format json`,
	RunE: listPlans,
}

func init() {
	plansCmd.AddCommand(plansShowCmd)
	plansCmd.AddCommand(plansListCmd)
	plansListCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	rootCmd.AddCommand(plansCmd)
}

// resolvePlanID strips an infer://plans/ prefix if present, returning just the ID.
func resolvePlanID(input string) string {
	const prefix = "infer://plans/"
	if strings.HasPrefix(input, prefix) {
		return strings.TrimPrefix(input, prefix)
	}
	return input
}

func showPlan(cmd *cobra.Command, args []string) error {
	storageConfig := storage.NewStorageFromConfig(Cfg)
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer func() {
		if closer, ok := stores.Plans.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	planID := resolvePlanID(args[0])
	plan, err := stores.Plans.LoadPlan(context.Background(), planID)
	if err != nil {
		return fmt.Errorf("failed to load plan %q: %w", planID, err)
	}

	printMarkdown("# " + plan.Title + "\n\n" + plan.Body)
	return nil
}

// printMarkdown renders md with glamour unless colors are disabled
// (--no-colors, non-TTY stdout, or NO_COLOR — decided once in
// rootCmd.PersistentPreRun), in which case it prints the raw markdown so
// piped output and agent Bash calls stay escape-free.
func printMarkdown(md string) {
	if !outputColorsDisabled {
		if rendered, err := renderMarkdown(md); err == nil {
			fmt.Print(rendered)
			return
		}
	}
	fmt.Println(md)
}

func listPlans(cmd *cobra.Command, args []string) error {
	storageConfig := storage.NewStorageFromConfig(Cfg)
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer func() {
		if closer, ok := stores.Plans.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}()

	plans, err := stores.Plans.ListPlans(context.Background())
	if err != nil {
		return fmt.Errorf("failed to list plans: %w", err)
	}

	format, _ := cmd.Flags().GetString("format")
	if format == "json" {
		return renderPlansJSON(plans)
	}

	return renderPlansTable(plans)
}

func renderPlansJSON(plans []*storage.PlanRecord) error {
	output := struct {
		Plans []*storage.PlanRecord `json:"plans"`
		Count int                   `json:"count"`
	}{
		Plans: plans,
		Count: len(plans),
	}

	jsonBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal plans to JSON: %w", err)
	}

	fmt.Println(string(jsonBytes))
	return nil
}

func renderPlansTable(plans []*storage.PlanRecord) error {
	if len(plans) == 0 {
		fmt.Println("No plans found.")
		return nil
	}

	var table strings.Builder
	table.WriteString("| ID | Title | Created At |\n")
	table.WriteString("|---|---|---|\n")
	for _, p := range plans {
		fmt.Fprintf(&table, "| %s | %s | %s |\n",
			p.ID, p.Title, p.CreatedAt.Format("2006-01-02 15:04:05"))
	}
	printMarkdown(table.String())
	return nil
}
