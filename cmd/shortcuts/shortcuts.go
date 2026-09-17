package shortcuts

import (
	"encoding/json"
	"fmt"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	shortcuts "github.com/inference-gateway/cli/internal/presentation/shortcuts"
)

// Entry is one registered slash command as `shortcuts list --format json`
// prints it.
type Entry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
}

// NewCommand constructs the shortcuts command tree.
func NewCommand(state *runtime.State) *cobra.Command {
	shortcutsCmd := &cobra.Command{
		Use:   "shortcuts",
		Short: "Inspect chat slash commands",
		Long: `Inspect the slash commands the chat accepts: the built-ins plus custom
shortcuts from .infer/shortcuts/*.yaml and ~/.infer/shortcuts/*.yaml.`,
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List available slash commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			return list(cmd, state.Config(), format)
		},
	}
	listCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
	shortcutsCmd.AddCommand(listCmd)
	return shortcutsCmd
}

// Entries lists every registered shortcut for cfg, sorted by name.
func Entries(cfg *config.Config) []Entry {
	all := shortcuts.NewMetadataRegistry(cfg).GetAll()
	entries := make([]Entry, 0, len(all))
	for _, sc := range all {
		entries = append(entries, Entry{Name: sc.GetName(), Description: sc.GetDescription(), Usage: sc.GetUsage()})
	}
	return entries
}

func list(cmd *cobra.Command, cfg *config.Config, format string) error {
	if cfg == nil {
		cfg = config.DefaultConfig()
	}
	if format == "json" {
		entries := Entries(cfg)
		data, err := json.MarshalIndent(map[string]any{"shortcuts": entries, "total": len(entries)}, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal shortcuts: %w", err)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), shortcuts.HelpText(shortcuts.NewMetadataRegistry(cfg)))
	return err
}
