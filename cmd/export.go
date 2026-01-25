package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tools "github.com/inference-gateway/cli/internal/agent/tools"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
	services "github.com/inference-gateway/cli/internal/services"
	styles "github.com/inference-gateway/cli/internal/ui/styles"
	cobra "github.com/spf13/cobra"
)

var exportCmd = &cobra.Command{
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
		return runExport(args[0])
	},
}

func init() {
	rootCmd.AddCommand(exportCmd)
}

func runExport(sessionID string) error {
	cfg, err := getConfigFromViper()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	storageConfig := storage.NewStorageFromConfig(cfg)
	storageBackend, err := storage.NewStorage(storageConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}

	configService := services.NewConfigService(V, cfg)
	toolRegistry := tools.NewRegistry(configService, nil, nil, nil, nil, nil)
	themeService := domain.NewThemeProvider()
	styleProvider := styles.NewProvider(themeService)
	toolFormatterService := services.NewToolFormatterService(toolRegistry, styleProvider)
	pricingService := services.NewPricingService(&cfg.Pricing)
	persistentRepo := services.NewPersistentConversationRepository(toolFormatterService, pricingService, storageBackend)

	ctx := context.Background()
	if err := persistentRepo.LoadConversation(ctx, sessionID); err != nil {
		return fmt.Errorf("failed to load session %s: %w", sessionID, err)
	}

	if persistentRepo.GetMessageCount() == 0 {
		return fmt.Errorf("no conversation to export - conversation history is empty")
	}

	data, err := persistentRepo.Export(domain.ExportMarkdown)
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
