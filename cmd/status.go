package cmd

import (
	"context"
	"fmt"
	"time"

	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	logger "github.com/inference-gateway/cli/internal/logger"
	cobra "github.com/spf13/cobra"
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
		fmt.Println("Checking inference gateway status...")

		_, _ = cmd.Flags().GetString("config")
		format, _ := cmd.Flags().GetString("format")

		gatewayURL := V.GetString("gateway.url")

		cfg := &config.Config{
			Gateway: config.GatewayConfig{
				URL:     gatewayURL,
				APIKey:  V.GetString("gateway.api_key"),
				Timeout: V.GetInt("gateway.timeout"),
			},
		}
		modelsResp, err := fetchModels(cfg)
		if err != nil {
			logger.Warn("Gateway unreachable", "error", err)
			fmt.Printf("Gateway Status: Unreachable (%v)\n", err)
			fmt.Println("Models: Unable to connect")
			return nil
		}

		modelCount := len(modelsResp.Data)

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
	services := container.NewServiceContainer(cfg)

	timeout := time.Duration(cfg.Gateway.Timeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		logger.Error("ListModels API call failed", "error", err)
		return nil, err
	}

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
