package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	config "github.com/inference-gateway/cli/config"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project with Inference Gateway CLI",
	Long: `Initialize a new project directory with Inference Gateway CLI configuration.
This creates the .infer directory with configuration file and additional setup files like .gitignore.

This is the recommended command to start working with Inference Gateway CLI in a new project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	initCmd.Flags().Bool("userspace", false, "Initialize configuration in user home directory (~/.infer/)")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	userspace, _ := cmd.Flags().GetBool("userspace")

	var configPath, gitignorePath string

	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		gitignorePath = filepath.Join(homeDir, config.ConfigDirName, config.GitignoreFileName)
	} else {
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
	}

	if !overwrite {
		if _, err := os.Stat(configPath); err == nil {
			return fmt.Errorf("configuration file %s already exists (use --overwrite to replace)", configPath)
		}
		if _, err := os.Stat(gitignorePath); err == nil {
			return fmt.Errorf(".gitignore file %s already exists (use --overwrite to replace)", gitignorePath)
		}
	}

	if err := writeConfigAsYAMLWithIndent(configPath, 2); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	gitignoreContent := `# Ignore log files and history files
logs/*.log
history
chat_export_*
`

	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore file: %w", err)
	}

	var scopeDesc string
	if userspace {
		scopeDesc = "userspace"
	} else {
		scopeDesc = "project"
	}

	fmt.Printf("%s Successfully initialized Inference Gateway CLI %s configuration\n", icons.CheckMarkStyle.Render(icons.CheckMark), scopeDesc)
	fmt.Printf("   Created: %s\n", configPath)
	fmt.Printf("   Created: %s\n", gitignorePath)
	fmt.Println("")
	if userspace {
		fmt.Println("This userspace configuration will be used as a fallback for all projects.")
		fmt.Println("Project-level configurations will take precedence when present.")
		fmt.Println("")
	}
	fmt.Println("You can now customize the configuration:")
	fmt.Println("  • Set default model: infer config agent set-model <model-name>")
	fmt.Println("  • Configure tools: infer config tools --help")
	fmt.Println("  • Start chatting: infer chat")

	return nil
}

// writeConfigAsYAMLWithIndent writes the current Viper configuration to a YAML file with specified indentation
func writeConfigAsYAMLWithIndent(filename string, indent int) error {
	allSettings := V.AllSettings()

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(allSettings); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	return os.WriteFile(filename, buf.Bytes(), 0644)
}
