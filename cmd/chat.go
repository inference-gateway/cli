package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/app"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with model selection",
	Long: `Start an interactive chat session where you can select a model from a dropdown
and have a conversational interface with the inference gateway.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startChatSession()
	},
}

// startChatSession starts a chat session using the SOLID architecture
func startChatSession() error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	services := container.NewServiceContainer(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	defaultModel := cfg.Chat.DefaultModel
	if defaultModel != "" {
		defaultModel = validateAndSetDefaultModel(services, models, defaultModel)
	}

	application := app.NewChatApplication(services, models, defaultModel)

	program := tea.NewProgram(application)

	fmt.Println("🤖 Starting chat session...")
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("error running chat interface: %w", err)
	}

	fmt.Println("👋 Chat session ended!")
	return nil
}

func validateAndSetDefaultModel(services *container.ServiceContainer, models []string, defaultModel string) string {
	modelFound := false
	for _, model := range models {
		if model == defaultModel {
			modelFound = true
			break
		}
	}

	if !modelFound {
		fmt.Printf("⚠️  Default model '%s' is not available, showing model selection...\n", defaultModel)
		return ""
	}

	if err := services.GetModelService().SelectModel(defaultModel); err != nil {
		fmt.Printf("⚠️  Failed to set default model: %v, showing model selection...\n", err)
		return ""
	}

	fmt.Printf("🤖 Using default model: %s\n", defaultModel)
	return defaultModel
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
