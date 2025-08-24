package cmd

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	app "github.com/inference-gateway/cli/internal/app"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	cobra "github.com/spf13/cobra"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with model selection",
	Long: `Start an interactive chat session where you can select a model from a dropdown
and have a conversational interface with the inference gateway.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return startChatSession(cfg)
	},
}

// startChatSession starts a chat session
func startChatSession(cfg *config.Config) error {
	services := container.NewServiceContainer(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.Gateway.Timeout)*time.Second)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	if len(models) == 0 {
		return fmt.Errorf("no models available from inference gateway")
	}

	defaultModel := cfg.Agent.Model
	if defaultModel != "" {
		defaultModel = validateAndSetDefaultModel(services.GetModelService(), models, defaultModel)
	}

	agentService := services.GetAgentService()
	conversationRepo := services.GetConversationRepository()
	modelService := services.GetModelService()
	config := services.GetConfig()
	toolService := services.GetToolService()
	fileService := services.GetFileService()
	commandRegistry := services.GetCommandRegistry()
	stateManager := services.GetStateManager()
	toolOrchestrator := services.GetToolExecutionOrchestrator()
	theme := services.GetTheme()
	toolRegistry := services.GetToolRegistry()

	application := app.NewChatApplication(
		models,
		defaultModel,
		agentService,
		conversationRepo,
		modelService,
		config,
		toolService,
		fileService,
		commandRegistry,
		stateManager,
		toolOrchestrator,
		theme,
		toolRegistry,
		V.ConfigFileUsed(),
	)

	program := tea.NewProgram(application)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("error running chat interface: %w", err)
	}

	fmt.Println("👋 Chat session ended!")
	return nil
}

func validateAndSetDefaultModel(modelService domain.ModelService, models []string, defaultModel string) string {
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

	if err := modelService.SelectModel(defaultModel); err != nil {
		fmt.Printf("⚠️  Failed to set default model: %v, showing model selection...\n", err)
		return ""
	}

	fmt.Printf("🤖 Using default model: %s\n", defaultModel)
	return defaultModel
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
