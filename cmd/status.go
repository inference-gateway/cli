package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/container"
	"github.com/inference-gateway/cli/internal/logger"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the status of the inference gateway",
	Long: `Display the current status of the inference gateway including:
- Running services
- Model deployments
- Health checks
- Resource usage`,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger.Debug("Starting status command")
		fmt.Println("Checking inference gateway status...")

		configPath, _ := cmd.Flags().GetString("config")
		format, _ := cmd.Flags().GetString("format")

		logger.Debug("Status command flags", "config_path", configPath, "format", format)

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			logger.Error("Failed to load config in status command", "error", err)
			return fmt.Errorf("failed to load config: %w", err)
		}

		logger.Debug("Fetching models from gateway", "gateway_url", cfg.Gateway.URL)
		modelsResp, err := fetchModels(cfg)
		if err != nil {
			logger.Warn("Gateway unreachable", "error", err)
			fmt.Printf("Gateway Status: Unreachable (%v)\n", err)
			fmt.Println("Models: Unable to connect")
			return nil
		}

		modelCount := len(modelsResp.Data)
		logger.Debug("Successfully fetched models", "count", modelCount)

		fmt.Println("Gateway Status: Running")
		fmt.Printf("Models: %d active\n", modelCount)

		if format != "text" {
			fmt.Printf("Output format: %s\n", format)
		}

		return nil
	},
}

// fetchModels retrieves the list of available models from the gateway
func fetchModels(cfg *config.Config) (*struct {
	Data []string `json:"data"`
}, error) {
	logger.Debug("Creating service container")
	services := container.NewServiceContainer(cfg)

	timeout := time.Duration(cfg.Gateway.Timeout) * time.Second
	logger.Debug("Creating request context", "timeout", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	logger.Debug("Calling ListModels API")
	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		logger.Error("ListModels API call failed", "error", err)
		return nil, err
	}

	logger.Debug("ListModels API call succeeded", "models", models)
	return &struct {
		Data []string `json:"data"`
	}{
		Data: models,
	}, nil
}

func init() {
	rootCmd.AddCommand(statusCmd)

	statusCmd.Flags().StringP("format", "f", "text", "Output format (text, json, yaml)")
}
