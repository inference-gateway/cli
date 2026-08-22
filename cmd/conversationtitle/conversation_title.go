package conversationtitle

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	container "github.com/inference-gateway/cli/internal/container"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "conversation-title",
		Short: "Manage conversation title generation",
		Long: `Manage conversation title generation including triggering manual title generation
for all conversations that need it.`,
	}

	generateCommand := &cobra.Command{
		Use:   "generate",
		Short: "Generate titles for conversations that need them",
		Long: `Generate AI-powered titles for conversations that either don't have generated titles
or have invalidated titles due to being resumed or modified.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateTitles(state)
		},
	}
	statusCommand := &cobra.Command{
		Use:   "status",
		Short: "Show conversation title generation status",
		Long:  `Show the status of conversation title generation including configuration and pending conversations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return showTitleStatus(state)
		},
	}
	daemonCommand := &cobra.Command{
		Use:   "daemon",
		Short: "Run conversation title generation daemon",
		Long:  `Run the background job manager as a daemon to continuously generate titles for conversations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTitleDaemon(state)
		},
	}

	command.AddCommand(generateCommand, statusCommand, daemonCommand)
	return command
}

func generateTitles(state *runtime.State) error {
	services := container.NewServiceContainer(state.Config())
	backgroundJobManager := services.GetBackgroundJobManager()

	if backgroundJobManager == nil {
		return fmt.Errorf("background job manager not available - enable persistent storage to use title generation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fmt.Println("🤖 Generating titles for conversations that need them...")

	start := time.Now()
	if err := backgroundJobManager.TriggerTitleGeneration(ctx); err != nil {
		return fmt.Errorf("failed to generate conversation titles: %w", err)
	}

	duration := time.Since(start)
	fmt.Printf("✅ Title generation completed in %v\n", duration.Round(time.Millisecond))

	return nil
}

func showTitleStatus(state *runtime.State) error {
	cfg := state.Config()
	services := container.NewServiceContainer(cfg)
	storage := services.GetStorage()
	backgroundJobManager := services.GetBackgroundJobManager()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("📝 Conversation Title Generation Status\n\n")

	fmt.Printf("Configuration:\n")
	fmt.Printf("  Enabled: %v\n", cfg.Conversation.TitleGeneration.Enabled)
	fmt.Printf("  Model: %s\n", cfg.Conversation.TitleGeneration.Model)
	fmt.Printf("  Batch Size: %d\n", cfg.Conversation.TitleGeneration.BatchSize)
	fmt.Printf("  Background Jobs Running: %v\n", backgroundJobManager != nil && backgroundJobManager.IsRunning())

	if storage == nil {
		return nil
	}

	pending, err := storage.ListConversationsNeedingTitles(ctx, 100)
	if err != nil {
		logger.Warn("failed to list conversations needing titles", "error", err)
		fmt.Printf("  Pending: Unable to retrieve (error: %v)\n", err)
		return nil
	}

	fmt.Printf("  Pending: %d conversations need titles\n", len(pending))
	if len(pending) == 0 {
		return nil
	}

	fmt.Printf("\nPending Conversations:\n")
	for i, conv := range pending {
		if i >= 10 {
			fmt.Printf("  ... and %d more\n", len(pending)-10)
			break
		}
		status := "new"
		if conv.TitleGenerated && conv.TitleInvalidated {
			status = "invalidated"
		}
		fmt.Printf("  - %s (%s, %d messages, %s)\n", conv.ID[:8], conv.Title, conv.MessageCount, status)
	}

	return nil
}

func runTitleDaemon(state *runtime.State) error {
	services := container.NewServiceContainer(state.Config())
	backgroundJobManager := services.GetBackgroundJobManager()

	if backgroundJobManager == nil {
		return fmt.Errorf("background job manager not available - enable persistent storage to use title generation")
	}

	if backgroundJobManager.IsRunning() {
		fmt.Println("⚠️  Background job manager is already running")
		return nil
	}

	fmt.Println("🚀 Starting conversation title generation daemon...")
	fmt.Println("📝 Press Ctrl+C to stop")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	backgroundJobManager.Start(ctx)

	<-sigChan
	fmt.Println("\n🛑 Shutting down daemon...")
	cancel()

	backgroundJobManager.Stop()
	fmt.Println("✅ Daemon stopped successfully")

	return nil
}
