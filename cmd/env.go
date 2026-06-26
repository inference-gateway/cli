package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// envExampleFileName is the name of the .env.example file
const envExampleFileName = ".env.example"

// envVars is the list of all provider API environment variables
var envVars = []string{
	"GOOGLE_SEARCH_API_KEY",
	"GOOGLE_SEARCH_ENGINE_ID",
	"",
	"DUCKDUCKGO_SEARCH_API_KEY",
	"",
	"ANTHROPIC_API_KEY",
	"CLOUDFLARE_API_KEY",
	"COHERE_API_KEY",
	"GROQ_API_KEY",
	"OLLAMA_API_KEY",
	"OLLAMA_CLOUD_API_KEY",
	"OPENAI_API_KEY",
	"DEEPSEEK_API_KEY",
	"GOOGLE_API_KEY",
	"MISTRAL_API_KEY",
	"MINIMAX_API_KEY",
	"MOONSHOT_API_KEY",
}

// envExampleContent generates the content for .env.example
func envExampleContent() string {
	var sb strings.Builder
	sb.WriteString("# Inference Gateway Environment Variables\n")
	sb.WriteString("# Copy this file to .env and fill in your API keys\n")
	sb.WriteString("#\n")
	sb.WriteString("#   cp .env.example .env\n")
	sb.WriteString("\n")

	for _, v := range envVars {
		if v == "" {
			sb.WriteString("\n")
		} else {
			sb.WriteString(fmt.Sprintf("%s=\n", v))
		}
	}

	return sb.String()
}

var envCmd = &cobra.Command{
	Use:   "env",
	Short: "Generate a .env.example file with provider API keys",
	Long: `Generate a .env.example file in the current directory with all the
different provider API environment variables needed by the Inference Gateway.

This is a convenient shortcut so you don't need to remember which providers
are available or what environment variables to set.

After the file is created, copy it to .env and fill in your API keys:

  cp .env.example .env

If .env.example already exists, this command will error. Use --overwrite to
replace it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return createEnvExample(cmd)
	},
}

func init() {
	envCmd.Flags().Bool("overwrite", false, "Overwrite .env.example if it already exists")
	rootCmd.AddCommand(envCmd)
}

// createEnvExample creates the .env.example file with provider API keys
func createEnvExample(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")

	envExamplePath := envExampleFileName

	// Check if .env.example already exists
	if !overwrite {
		if _, err := os.Stat(envExamplePath); err == nil {
			return fmt.Errorf("%s %s already exists (use --overwrite to replace)", envExampleFileName, envExamplePath)
		}
	}

	// Create .env.example
	content := envExampleContent()
	if err := os.WriteFile(envExamplePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create %s file: %w", envExampleFileName, err)
	}

	fmt.Printf("%s Successfully created %s\n", icons.CheckMarkStyle.Render(icons.CheckMark), envExamplePath)
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  cp .env.example .env")
	fmt.Println("  Edit .env and add your API keys")
	fmt.Println("")

	// Check if .gitignore exists, if not create one with .env entry
	gitignorePath := filepath.Join(config.ConfigDirName, config.GitignoreFileName)
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		// Check if there's a root .gitignore
		rootGitignore := config.GitignoreFileName
		if _, err := os.Stat(rootGitignore); os.IsNotExist(err) {
			gitignoreContent := "# Inference Gateway\n.env\n"
			if err := os.WriteFile(rootGitignore, []byte(gitignoreContent), 0644); err != nil {
				return fmt.Errorf("failed to create .gitignore file: %w", err)
			}
			fmt.Printf("%s Created .gitignore with .env entry\n", icons.CheckMarkStyle.Render(icons.CheckMark))
		} else if err == nil {
			// .gitignore exists, check if .env is already in it
			data, err := os.ReadFile(rootGitignore)
			if err == nil && !strings.Contains(string(data), ".env") {
				f, err := os.OpenFile(rootGitignore, os.O_APPEND|os.O_WRONLY, 0644)
				if err == nil {
					_, _ = f.WriteString("\n# Inference Gateway\n.env\n")
					f.Close()
					fmt.Printf("%s Added .env to .gitignore\n", icons.CheckMarkStyle.Render(icons.CheckMark))
				}
			}
		}
	}

	return nil
}
