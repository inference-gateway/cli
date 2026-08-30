package configcmd

import (
	"fmt"
	"os"
	"path/filepath"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	configutils "github.com/inference-gateway/cli/config/utils"
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
		Long:  `Manage the Inference Gateway CLI configuration settings.`,
	}
	command.AddCommand(newInitCommand(), newGetCommand(state), newSetCommand())
	return command
}

func newInitCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new configuration file",
		Long: `Initialize the userspace baseline ~/.infer/config.yaml with default settings.

A project ./.infer/config.yaml is an override layer, not a second full config:
create it with 'infer config set --project <key> <value>', which writes only the
keys you set. Seeding a full default config into a project would shadow the
entire userspace baseline, because project values replace userspace ones
key-by-key (and project lists replace, rather than extend, userspace lists).

For complete initialization, use 'infer init' instead.`,
		RunE: initialize,
	}
	command.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	return command
}

func initialize(cmd *cobra.Command, _ []string) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get user home directory: %w", err)
	}
	configPath := filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)

	if _, err := os.Stat(configPath); err == nil && !overwrite {
		return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
	}
	if err := configutils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	fmt.Printf("Successfully created userspace configuration: %s\n", configPath)
	fmt.Println("This userspace configuration is the shared baseline for all your projects.")
	fmt.Println("To override a setting for one project: infer config set --project <key> <value>")
	fmt.Println("Tip: Use 'infer init' for complete initialization including additional setup files.")
	return nil
}
