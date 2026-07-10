package cmd

import (
	"fmt"
	"strings"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	keybinding "github.com/inference-gateway/cli/internal/ui/keybinding"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

var keybindingsCmd = &cobra.Command{
	Use:   "keybindings",
	Short: "Manage keybinding configuration",
	Long:  `Manage custom keybindings for the chat interface. Customize keys and enable/disable specific actions.`,
}

var keybindingsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available keybindings",
	Long:  `Display all available keybinding actions with their current key assignments and enabled status.`,
	RunE:  listKeybindings,
}

var keybindingsResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset keybindings to defaults",
	Long:  `Reset all keybindings to their default values by removing custom overrides from configuration.`,
	RunE:  resetKeybindings,
}

var keybindingsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate keybinding configuration",
	Long:  `Validate the current keybinding configuration for conflicts and errors.`,
	RunE:  validateKeybindings,
}

var keybindingsSetCmd = &cobra.Command{
	Use:   "set <action-id> <key1> [key2...]",
	Short: "Set custom keys for an action",
	Long: `Set custom keybinding(s) for a specific action. You can specify multiple keys separated by spaces.

Example:
  infer keybindings set cycle_agent_mode ctrl+m
  infer keybindings set send_message ctrl+enter enter`,
	Args: cobra.MinimumNArgs(2),
	RunE: setKeybinding,
}

var keybindingsEnableCmd = &cobra.Command{
	Use:   "enable <action-id>",
	Short: "Enable a keybinding action",
	Long:  `Enable a specific keybinding action.`,
	Args:  cobra.ExactArgs(1),
	RunE:  enableKeybinding,
}

var keybindingsDisableCmd = &cobra.Command{
	Use:   "disable <action-id>",
	Short: "Disable a keybinding action",
	Long:  `Disable a specific keybinding action.`,
	Args:  cobra.ExactArgs(1),
	RunE:  disableKeybinding,
}

func init() {
	keybindingsCmd.AddCommand(keybindingsListCmd)
	keybindingsCmd.AddCommand(keybindingsResetCmd)
	keybindingsCmd.AddCommand(keybindingsValidateCmd)
	keybindingsCmd.AddCommand(keybindingsSetCmd)
	keybindingsCmd.AddCommand(keybindingsEnableCmd)
	keybindingsCmd.AddCommand(keybindingsDisableCmd)

	keybindingsCmd.PersistentFlags().Bool("project", false, "Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")

	rootCmd.AddCommand(keybindingsCmd)
}

func listKeybindings(cmd *cobra.Command, args []string) error {
	registry := keybinding.NewRegistry(Cfg)
	actions := registry.ListAllActions()

	if len(actions) == 0 {
		fmt.Println("No keybindings found.")
		return nil
	}

	fmt.Printf("KEYBINDINGS (%d total)\n", len(actions))
	fmt.Printf("══════════════════════\n\n")

	currentCategory := ""
	for _, action := range actions {
		if action.Category != currentCategory {
			currentCategory = action.Category
			fmt.Printf("%s\n", strings.ToUpper(currentCategory))
			fmt.Printf("──────────────────\n")
		}

		status := icons.CheckMarkStyle.Render(icons.CheckMark)
		if !action.Binding.Enabled() {
			status = icons.CrossMarkStyle.Render(icons.CrossMark)
		}

		keys := strings.Join(action.Binding.Keys(), ", ")
		fmt.Printf("  %s %-25s %s\n", status, action.ID, keys)
		fmt.Printf("     %s\n", action.Binding.Help().Desc)
		fmt.Println()
	}

	fmt.Printf("\n%s = enabled, %s = disabled\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		icons.CrossMarkStyle.Render(icons.CrossMark))
	fmt.Printf("\nTo customize: infer keybindings set <action-id> <key>\n")
	fmt.Printf("To disable: infer keybindings disable <action-id>\n")

	return nil
}

func resetKeybindings(cmd *cobra.Command, args []string) error {
	path, err := getKeybindingsConfigWritePath(GetProjectFlag(cmd))
	if err != nil {
		return err
	}

	if err := config.SaveKeybindings(path, config.DefaultKeybindingsConfig()); err != nil {
		return fmt.Errorf("failed to save keybindings: %w", err)
	}

	fmt.Printf("%s Keybindings reset to defaults\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	fmt.Printf("Configuration saved to %s\n", path)

	return nil
}

func validateKeybindings(cmd *cobra.Command, args []string) error {
	cfg := Cfg

	if !cfg.Chat.Keybindings.Enabled {
		fmt.Println("Note: Keybindings are currently disabled in config")
		fmt.Println()
	}

	validActions := getValidActionIDs()
	hasErrors := false

	hasErrors = validateUnknownActions(cfg, validActions) || hasErrors
	hasErrors = validateInvalidKeys(cfg, validActions) || hasErrors
	hasErrors = validateKeyConflicts(cfg, validActions) || hasErrors

	if hasErrors {
		fmt.Println("Run 'infer keybindings list' to see available actions and keys.")
		return fmt.Errorf("keybinding validation failed")
	}

	fmt.Printf("%s Keybinding configuration is valid\n",
		icons.CheckMarkStyle.Render(icons.CheckMark))
	fmt.Printf("Total configured bindings: %d\n", len(cfg.Chat.Keybindings.Bindings))

	return nil
}

func validateUnknownActions(cfg *config.Config, validActions []string) bool {
	unknownActions := []string{}
	for actionID := range cfg.Chat.Keybindings.Bindings {
		if !contains(validActions, actionID) {
			unknownActions = append(unknownActions, actionID)
		}
	}

	if len(unknownActions) > 0 {
		fmt.Printf("%s Found unknown action IDs:\n",
			icons.CrossMarkStyle.Render(icons.CrossMark))
		for _, actionID := range unknownActions {
			fmt.Printf("  - %s\n", actionID)
		}
		fmt.Println()
		return true
	}
	return false
}

func validateInvalidKeys(cfg *config.Config, validActions []string) bool {
	invalidKeys := make(map[string][]string)
	for actionID, binding := range cfg.Chat.Keybindings.Bindings {
		if !contains(validActions, actionID) {
			continue
		}
		for _, key := range binding.Keys {
			if !keybinding.IsValidKey(key) {
				invalidKeys[actionID] = append(invalidKeys[actionID], key)
			}
		}
	}

	if len(invalidKeys) > 0 {
		fmt.Printf("%s Found invalid keys:\n",
			icons.CrossMarkStyle.Render(icons.CrossMark))
		for actionID, keys := range invalidKeys {
			fmt.Printf("  - %s: %v\n", actionID, keys)
		}
		fmt.Println()
		return true
	}
	return false
}

func validateKeyConflicts(cfg *config.Config, validActions []string) bool {
	keyUsage := buildKeyUsageMap(cfg, validActions)
	conflicts := findNamespaceConflicts(keyUsage)

	if len(conflicts) > 0 {
		fmt.Printf("%s Found key conflicts within same namespace:\n",
			icons.CrossMarkStyle.Render(icons.CrossMark))
		for key, actions := range conflicts {
			fmt.Printf("  - %s is bound to: %v\n", key, actions)
		}
		fmt.Println()
		return true
	}
	return false
}

func buildKeyUsageMap(cfg *config.Config, validActions []string) map[string][]string {
	keyUsage := make(map[string][]string)
	for actionID, binding := range cfg.Chat.Keybindings.Bindings {
		if !contains(validActions, actionID) {
			continue
		}
		for _, key := range binding.Keys {
			if keybinding.IsValidKey(key) {
				keyUsage[key] = append(keyUsage[key], actionID)
			}
		}
	}
	return keyUsage
}

func findNamespaceConflicts(keyUsage map[string][]string) map[string][]string {
	conflicts := make(map[string][]string)
	for key, actions := range keyUsage {
		if len(actions) <= 1 {
			continue
		}

		namespaceMap := make(map[string][]string)
		for _, actionID := range actions {
			parts := strings.Split(actionID, "_")
			if len(parts) >= 2 {
				namespace := parts[0]
				namespaceMap[namespace] = append(namespaceMap[namespace], actionID)
			}
		}

		for namespace, nsActions := range namespaceMap {
			if len(nsActions) > 1 {
				conflictKey := fmt.Sprintf("%s (namespace: %s)", key, namespace)
				conflicts[conflictKey] = nsActions
			}
		}
	}
	return conflicts
}

func setKeybinding(cmd *cobra.Command, args []string) error {
	actionID := args[0]
	keys := args[1:]

	validActions := getValidActionIDs()
	if !contains(validActions, actionID) {
		return fmt.Errorf("unknown action '%s'. Run 'infer keybindings list' to see available actions", actionID)
	}

	path, kbConfig, err := loadKeybindingsForWrite(cmd)
	if err != nil {
		return err
	}

	entry := kbConfig.Bindings[actionID]
	entry.Keys = keys
	kbConfig.Bindings[actionID] = entry

	if err := config.SaveKeybindings(path, kbConfig); err != nil {
		return fmt.Errorf("failed to save keybindings: %w", err)
	}

	fmt.Printf("%s Keybinding updated: %s → %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		actionID,
		strings.Join(keys, ", "))
	fmt.Printf("Configuration saved to %s\n", path)

	return nil
}

func enableKeybinding(cmd *cobra.Command, args []string) error {
	actionID := args[0]

	validActions := getValidActionIDs()
	if !contains(validActions, actionID) {
		return fmt.Errorf("unknown action '%s'. Run 'infer keybindings list' to see available actions", actionID)
	}

	path, kbConfig, err := loadKeybindingsForWrite(cmd)
	if err != nil {
		return err
	}

	entry := kbConfig.Bindings[actionID]
	enabled := true
	entry.Enabled = &enabled
	kbConfig.Bindings[actionID] = entry

	if err := config.SaveKeybindings(path, kbConfig); err != nil {
		return fmt.Errorf("failed to save keybindings: %w", err)
	}

	fmt.Printf("%s Keybinding enabled: %s\n",
		icons.CheckMarkStyle.Render(icons.CheckMark),
		actionID)
	fmt.Printf("Configuration saved to %s\n", path)

	return nil
}

func disableKeybinding(cmd *cobra.Command, args []string) error {
	actionID := args[0]

	validActions := getValidActionIDs()
	if !contains(validActions, actionID) {
		return fmt.Errorf("unknown action '%s'. Run 'infer keybindings list' to see available actions", actionID)
	}

	path, kbConfig, err := loadKeybindingsForWrite(cmd)
	if err != nil {
		return err
	}

	entry := kbConfig.Bindings[actionID]
	disabled := false
	entry.Enabled = &disabled
	kbConfig.Bindings[actionID] = entry

	if err := config.SaveKeybindings(path, kbConfig); err != nil {
		return fmt.Errorf("failed to save keybindings: %w", err)
	}

	fmt.Printf("%s Keybinding disabled: %s\n",
		icons.CrossMarkStyle.Render(icons.CrossMark),
		actionID)
	fmt.Printf("Configuration saved to %s\n", path)

	return nil
}

// loadKeybindingsForWrite resolves the destination keybindings.yaml path
// (userspace ~/.infer/ by default, or the project layer under --project),
// loads the existing config (or defaults if the file is absent), and returns
// everything callers need to mutate-and-save.
func loadKeybindingsForWrite(cmd *cobra.Command) (string, *config.KeybindingsConfig, error) {
	path, err := getKeybindingsConfigWritePath(GetProjectFlag(cmd))
	if err != nil {
		return "", nil, err
	}

	kbConfig, err := config.LoadKeybindings(path)
	if err != nil {
		return "", nil, fmt.Errorf("failed to load keybindings: %w", err)
	}

	if kbConfig.Bindings == nil {
		kbConfig.Bindings = make(map[string]config.KeyBindingEntry)
	}

	return path, kbConfig, nil
}

// getValidActionIDs returns every recognised action ID: runtime registry actions
// plus the namespace-path actions from the default keybindings config (which are
// resolved directly by components via config.ResolveNamespaceBindings). Sharing this
// union with the runtime keeps `infer keybindings validate/set/enable/disable` in
// agreement with what actually takes effect.
func getValidActionIDs() []string {
	return keybinding.NewRegistry(config.DefaultConfig()).KnownActionIDs()
}
