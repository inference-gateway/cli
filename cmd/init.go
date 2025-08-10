package cmd

import (
	"fmt"
	"os"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project configuration",
	Long: `Initialize a new .infer/config.yaml configuration file in the current directory.
This creates a local project configuration with default settings.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := ".infer/config.yaml"

		if _, err := os.Stat(configPath); err == nil {
			overwrite, _ := cmd.Flags().GetBool("overwrite")
			if !overwrite {
				return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
			}
		}

		cfg := config.DefaultConfig()

		if err := cfg.SaveConfig(configPath); err != nil {
			return fmt.Errorf("failed to create config file: %w", err)
		}

		fmt.Printf("Successfully created %s\n", configPath)
		fmt.Println("You can now customize the configuration for this project.")

		return nil
	},
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().Bool("overwrite", false, "Overwrite existing configuration file")
}
