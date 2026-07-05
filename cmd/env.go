package cmd

import (
	"fmt"
	"os"
	"strings"

	cobra "github.com/spf13/cobra"

	config "github.com/inference-gateway/cli/config"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// envExampleFileName is the name of the .env.example file
const envExampleFileName = ".env.example"

// envVars is the list of all provider API environment variables.
// Empty-string entries produce blank-line separators in the output,
// grouping search-related keys separately from provider API keys.
var envVars = []string{
	"GOOGLE_SEARCH_API_KEY",
	"GOOGLE_SEARCH_ENGINE_ID",
	"", // --- search providers ---
	"DUCKDUCKGO_SEARCH_API_KEY",
	"", // --- LLM providers ---
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
	"NVIDIA_API_KEY",
	"NVIDIA_API_URL",
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
			fmt.Fprintf(&sb, "%s=\n", v)
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

	if !overwrite {
		if _, err := os.Stat(envExamplePath); err == nil {
			return fmt.Errorf("%s already exists (use --overwrite to replace)", envExamplePath)
		}
	}

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

	// Ensure .env is in .gitignore
	if err := ensureEnvInGitignore(); err != nil {
		return err
	}

	return nil
}

// ensureEnvInGitignore ensures that .env is listed in the root .gitignore file.
// If .gitignore doesn't exist, it creates one with .env entry.
// If .gitignore exists but doesn't contain .env, it appends it.
func ensureEnvInGitignore() error {
	gitignorePath := config.GitignoreFileName
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		gitignoreContent := "# Inference Gateway\n.env\n"
		if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
			return fmt.Errorf("failed to create .gitignore file: %w", err)
		}
		fmt.Printf("%s Created .gitignore with .env entry\n", icons.CheckMarkStyle.Render(icons.CheckMark))
		return nil
	}

	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		return fmt.Errorf("failed to read .gitignore: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == ".env" {
			return nil
		}
	}

	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open .gitignore: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	if _, err := f.WriteString("\n# Inference Gateway\n.env\n"); err != nil {
		return fmt.Errorf("failed to write to .gitignore: %w", err)
	}
	fmt.Printf("%s Added .env to .gitignore\n", icons.CheckMarkStyle.Render(icons.CheckMark))
	return nil
}
