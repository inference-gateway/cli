package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uuid "github.com/google/uuid"
	cobra "github.com/spf13/cobra"
	viper "github.com/spf13/viper"

	config "github.com/inference-gateway/cli/config"
	app "github.com/inference-gateway/cli/internal/app"
	clipboard "github.com/inference-gateway/cli/internal/clipboard"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	screenshotsvc "github.com/inference-gateway/cli/internal/services"
	tools "github.com/inference-gateway/cli/internal/services/tools"
	web "github.com/inference-gateway/cli/internal/web"
	sdk "github.com/inference-gateway/sdk"
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

		if os.Getenv("INFER_WEB_MODE") == "true" {
			cfg.Web.Enabled = true
			V.Set("web.enabled", true)
		}

		webMode, _ := cmd.Flags().GetBool("web")
		if webMode {
			cfg.Web.Enabled = true
			V.Set("web.enabled", true)

			if cmd.Flags().Changed("port") {
				cfg.Web.Port, _ = cmd.Flags().GetInt("port")
			}
			if cmd.Flags().Changed("host") {
				cfg.Web.Host, _ = cmd.Flags().GetString("host")
			}

			if cmd.Flags().Changed("ssh-host") {
				cfg.Web.SSH.Enabled = true
				sshHost, _ := cmd.Flags().GetString("ssh-host")
				sshUser, _ := cmd.Flags().GetString("ssh-user")
				sshPort, _ := cmd.Flags().GetInt("ssh-port")
				sshCommand, _ := cmd.Flags().GetString("ssh-command")
				noInstall, _ := cmd.Flags().GetBool("ssh-no-install")

				cfg.Web.Servers = []config.SSHServerConfig{
					{
						Name:        "CLI Remote Server",
						ID:          "cli-remote",
						RemoteHost:  sshHost,
						RemotePort:  sshPort,
						RemoteUser:  sshUser,
						CommandPath: sshCommand,
						AutoInstall: func() *bool { b := !noInstall; return &b }(),
						Description: "Remote server configured via CLI flags",
					},
				}
			}

			return StartWebChatSession(cfg, V)
		}

		if !isInteractiveTerminal() {
			return runNonInteractiveChat(cfg, V)
		}

		return StartChatSession(cfg, V)
	},
}

// StartChatSession starts a chat session
func StartChatSession(cfg *config.Config, v *viper.Viper) error {
	_ = clipboard.Init()

	services := container.NewServiceContainer(cfg, v)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	shutdownComplete := make(chan struct{})
	var shutdownOnce sync.Once

	doShutdown := func() {
		shutdownOnce.Do(func() {
			logger.Info("Received shutdown signal, cleaning up...")
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if err := services.Shutdown(ctx); err != nil {
				logger.Error("Error during shutdown", "error", err)
			}
			close(shutdownComplete)
		})
	}

	defer doShutdown()

	go func() {
		<-sigChan
		doShutdown()
		os.Exit(0)
	}()

	if err := services.GetGatewayManager().EnsureStarted(); err != nil {
		fmt.Printf("\n⚠️  Failed to start gateway automatically: %v\n", err)
		fmt.Printf("   Continuing without local gateway.\n")
		fmt.Printf("   Make sure the inference gateway is running at: %s\n\n", cfg.Gateway.URL)
	}

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
	configService := services.GetConfigService()
	toolService := services.GetToolService()
	fileService := services.GetFileService()
	imageService := services.GetImageService()
	pricingService := services.GetPricingService()
	shortcutRegistry := services.GetShortcutRegistry()
	stateManager := services.GetStateManager()
	messageQueue := services.GetMessageQueue()
	themeService := services.GetThemeService()
	toolRegistry := services.GetToolRegistry()
	mcpManager := services.GetMCPManager()
	taskRetentionService := services.GetTaskRetentionService()
	backgroundTaskService := services.GetBackgroundTaskService()
	agentManager := services.GetAgentManager()
	conversationOptimizer := services.GetConversationOptimizer()

	var screenshotServer *screenshotsvc.ScreenshotServer
	logger.Info("Checking screenshot streaming config",
		"computer_use_enabled", config.ComputerUse.Enabled,
		"screenshot_enabled", config.ComputerUse.Screenshot.Enabled,
		"streaming_enabled", config.ComputerUse.Screenshot.StreamingEnabled)

	if config.ComputerUse.Enabled && config.ComputerUse.Screenshot.StreamingEnabled {
		screenshotServer = startScreenshotServer(config, imageService, toolRegistry)
		if screenshotServer != nil {
			defer func() {
				if err := screenshotServer.Stop(); err != nil {
					logger.Error("Failed to stop screenshot server", "error", err)
				}
			}()
		}
	}

	versionInfo := GetVersionInfo()
	application := app.NewChatApplication(
		models,
		defaultModel,
		agentService,
		conversationRepo,
		conversationOptimizer,
		modelService,
		configService,
		toolService,
		fileService,
		imageService,
		pricingService,
		shortcutRegistry,
		stateManager,
		messageQueue,
		themeService,
		toolRegistry,
		mcpManager,
		taskRetentionService,
		backgroundTaskService,
		agentManager,
		getEffectiveConfigPath(),
		versionInfo,
	)

	program := tea.NewProgram(
		application,
		tea.WithMouseCellMotion(),
		tea.WithReportFocus(),
	)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("error running chat interface: %w", err)
	}

	application.PrintConversationHistory()

	fmt.Println("• Chat session ended!")
	return nil
}

// StartWebChatSession starts a web-based chat session with PTY and WebSocket
func StartWebChatSession(cfg *config.Config, v *viper.Viper) error {
	server := web.NewWebTerminalServer(cfg, v)
	return server.Start()
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
		fmt.Printf("• Default model '%s' is not available, showing model selection...\n", defaultModel)
		return ""
	}

	if err := modelService.SelectModel(defaultModel); err != nil {
		fmt.Printf("• Failed to set default model: %v, showing model selection...\n", err)
		return ""
	}

	fmt.Printf("• Using default model: %s\n", defaultModel)
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
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = services.Shutdown(ctx)
	}()

	if err := services.GetGatewayManager().EnsureStarted(); err != nil {
		return fmt.Errorf("failed to start gateway: %w", err)
	}

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
		Content: sdk.NewMessageContent(input),
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

// startScreenshotServer initializes and starts the screenshot streaming server
func startScreenshotServer(config *config.Config, imageService domain.ImageService, toolRegistry *tools.Registry) *screenshotsvc.ScreenshotServer {
	logger.Info("Screenshot streaming conditions met, starting server")
	sessionID := fmt.Sprintf("%d-%s", time.Now().Unix(), uuid.New().String()[:8])
	screenshotServer := screenshotsvc.NewScreenshotServer(config, imageService, sessionID)

	if err := screenshotServer.Start(); err != nil {
		logger.Warn("Failed to start screenshot server", "error", err)
		return nil
	}

	fmt.Printf("• Screenshot API: http://localhost:%d\n", screenshotServer.Port())
	toolRegistry.SetScreenshotServer(screenshotServer)
	logger.Info("Registered GetLatestScreenshot tool with tool registry")

	if os.Getenv("INFER_GATEWAY_MODE") == "remote" {
		fmt.Printf("\x1b]5555;screenshot_port=%d\x07", screenshotServer.Port())
	}

	return screenshotServer
}

func init() {
	rootCmd.AddCommand(chatCmd)
	chatCmd.Flags().Bool("web", false, "Start web terminal interface")
	chatCmd.Flags().Int("port", 0, "Web server port (default: 3000)")
	chatCmd.Flags().String("host", "", "Web server host (default: localhost)")
	chatCmd.Flags().String("ssh-host", "", "Remote SSH server hostname")
	chatCmd.Flags().String("ssh-user", "", "Remote SSH username")
	chatCmd.Flags().Int("ssh-port", 22, "Remote SSH port")
	chatCmd.Flags().Bool("ssh-no-install", false, "Disable auto-installation of infer on remote")
	chatCmd.Flags().String("ssh-command", "infer", "Path to infer binary on remote")
}
