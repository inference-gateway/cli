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
	command.PersistentFlags().Bool("project", false,
		"Apply to the project configuration (./.infer/) instead of the userspace baseline (~/.infer/)")
	command.AddCommand(newInitCommand(), newGetCommand(state), newSetCommand())
	return command
}

func newInitCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new configuration file",
		Long: `Initialize a new config.yaml with default settings.

By default this writes the userspace baseline at ~/.infer/config.yaml. Pass
--project to write a project-level ./.infer/config.yaml that overrides the
baseline key-by-key instead.

For complete project initialization, use 'infer init' instead.`,
		RunE: initialize,
	}
	command.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
	return command
}

func initialize(cmd *cobra.Command, _ []string) error {
	project := runtime.ProjectFlag(cmd)
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	configPath := config.DefaultConfigPath
	if !project {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
	}
	if _, err := os.Stat(configPath); err == nil && !overwrite {
		return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
	}
	if err := configutils.SaveYAML(configPath, "config", config.DefaultConfig()); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	scope := "userspace "
	if project {
		scope = "project "
	}
	fmt.Printf("Successfully created %sconfiguration: %s\n", scope, configPath)
	if project {
		fmt.Println("This project configuration overrides your userspace baseline (~/.infer/) key-by-key.")
	} else {
		fmt.Println("This userspace configuration is the shared baseline for all your projects.")
		fmt.Println("Project-level configurations are merged on top when present.")
	}
	fmt.Println("Tip: Use 'infer init' for complete project initialization including additional setup files.")
	return nil
}
