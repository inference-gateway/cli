package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	config "github.com/inference-gateway/cli/config"
	app "github.com/inference-gateway/cli/internal/app"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
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

		if !isInteractiveTerminal() {
			return runNonInteractiveChat(cfg, V)
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
	messageQueue := services.GetMessageQueue()
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
		messageQueue,
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

// isInteractiveTerminal checks if we're running in an interactive terminal
func isInteractiveTerminal() bool {
	if fileInfo, _ := os.Stdin.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	if fileInfo, _ := os.Stdout.Stat(); (fileInfo.Mode() & os.ModeCharDevice) == 0 {
		return false
	}
	return true
}

// runNonInteractiveChat handles non-interactive chat mode (stdin/stdout)
func runNonInteractiveChat(cfg *config.Config, v *viper.Viper) error {
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

	if defaultModel == "" || !contains(models, defaultModel) {
		if defaultModel != "" {
			fmt.Fprintf(os.Stderr, "Model %s not available, using: %s\n", defaultModel, models[0])
		} else {
			fmt.Fprintf(os.Stderr, "Using model: %s\n", models[0])
		}
		defaultModel = models[0]
	} else {
		fmt.Fprintf(os.Stderr, "Using model: %s\n", defaultModel)
	}

	if err := services.GetModelService().SelectModel(defaultModel); err != nil {
		return fmt.Errorf("failed to set model: %w", err)
	}

	scanner := bufio.NewScanner(os.Stdin)
	var inputLines []string
	for scanner.Scan() {
		inputLines = append(inputLines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading from stdin: %w", err)
	}

	if len(inputLines) == 0 {
		return fmt.Errorf("no input provided")
	}

	input := strings.Join(inputLines, "\n")

	userMessage := sdk.Message{
		Role:    sdk.User,
		Content: input,
	}

	req := &domain.AgentRequest{
		RequestID: fmt.Sprintf("req_%d", time.Now().UnixNano()),
		Model:     defaultModel,
		Messages:  []sdk.Message{userMessage},
	}

	agentService := services.GetAgentService()
	events, err := agentService.RunWithStream(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to start chat: %w", err)
	}

	return processStreamingOutput(events)
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// processStreamingOutput handles streaming output for non-interactive mode
func processStreamingOutput(events <-chan domain.ChatEvent) error {
	for event := range events {
		switch e := event.(type) {
		case domain.ChatChunkEvent:
			if e.Content != "" {
				fmt.Print(e.Content)
				_ = os.Stdout.Sync()
			}
		case domain.ChatCompleteEvent:
			fmt.Print("\n")
			_ = os.Stdout.Sync()
			return nil
		case domain.ChatErrorEvent:
			return fmt.Errorf("chat error: %v", e.Error)
		}
	}
	return nil
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
