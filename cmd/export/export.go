package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	storage "github.com/inference-gateway/cli/internal/platform/storage"
	styles "github.com/inference-gateway/cli/internal/presentation/tui/styles"
	toolformatter "github.com/inference-gateway/cli/internal/presentation/tui/toolformatter"
)

func NewCommand(state *runtime.State) *cobra.Command {
	return &cobra.Command{
		Use:   "export <session-id>",
		Short: "Export conversation to markdown",
		Long:  `Export a conversation session to a markdown file.`,
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("session ID required. Provide as argument: infer export <session-id>")
			}
			if args[0] == "" {
				return fmt.Errorf("no conversation to export. Send at least one message first, then use /export")
			}
			return runExport(state, args[0])
		},
	}
}

func runExport(state *runtime.State, sessionID string) error {
	cfg := state.Config()

	storageConfig := storage.NewStorageFromConfig(cfg)
	stores, err := storage.NewStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	toolRegistry := tools.NewRegistry(cfg, nil, nil, nil, nil, nil, nil)
	themeService := styles.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	toolFormatterService := toolformatter.NewToolFormatterService(toolRegistry, styleProvider)
	pricingService := conversation.NewPricingService(&cfg.Pricing)
	persistentRepo := conversation.NewPersistentConversationRepository(toolFormatterService, pricingService, stores.Conversations)

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to load session %s: %w", sessionID, err)
	}

	if persistentRepo.GetMessageCount() == 0 {
		return fmt.Errorf("no conversation to export - conversation history is empty")
	}

	data, err := persistentRepo.Export(convdomain.ExportMarkdown)
	if err != nil {
		return fmt.Errorf("failed to export conversation: %w", err)
	}

	outputDir := cfg.Export.OutputDir
	if outputDir == "" {
		outputDir = ".infer"
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	filename := fmt.Sprintf("chat_export_%s.md", time.Now().Format("20060102_150405"))
	filePath := filepath.Join(outputDir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	fmt.Printf("• Conversation exported to: %s\n", filePath)
	return nil
}
