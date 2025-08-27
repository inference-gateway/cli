package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	app "github.com/inference-gateway/cli/internal/app"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"
)

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with model selection",
	Long: `Start an interactive chat session where you can select a model from a dropdown
and have a conversational interface with the inference gateway.`,
	RunE: func(_ *cobra.Command, args []string) error {
		cfg, err := getConfigFromViper()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		return StartChatSession(cfg, V)
	},
}

// StartChatSession starts a chat session
func StartChatSession(cfg *config.Config, v *viper.Viper) error {
	services := container.NewServiceContainer(cfg, v)

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
	shortcutRegistry := services.GetShortcutRegistry()
	stateManager := services.GetStateManager()
	toolOrchestrator := services.GetToolExecutionOrchestrator()
	theme := services.GetTheme()
	themeService := services.GetThemeService()
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
		shortcutRegistry,
		stateManager,
		toolOrchestrator,
		theme,
		themeService,
		toolRegistry,
		getEffectiveConfigPath(),
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

// getEffectiveConfigPath returns the actual config file path that should be displayed
// It follows Viper's search order and returns the first existing config file
func getEffectiveConfigPath() string {
	searchPaths := []string{
		".infer/config.yaml",
	}

	if homeDir, err := os.UserHomeDir(); err == nil {
		homePath := filepath.Join(homeDir, ".infer", "config.yaml")
		searchPaths = append(searchPaths, homePath)
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	if configFile := V.ConfigFileUsed(); configFile != "" {
		return configFile
	}

	return ".infer/config.yaml"
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
