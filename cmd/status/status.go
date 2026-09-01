package status

import (
	"context"
	"fmt"
	"time"

	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	gateway "github.com/inference-gateway/cli/internal/gateway"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
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

			loaded := state.Config()

			cfg := &config.Config{
				Gateway: loaded.Gateway,
			}
			modelsResp, err := fetchModels(cfg)
			if err != nil {
				logger.Warn("gateway unreachable", "error", err)
				fmt.Printf("Gateway URL: %s\n", cfg.Gateway.URL)
				fmt.Printf("Gateway Status: Unreachable (%v)\n", err)
				fmt.Println("Version: unknown")
				fmt.Println("Models: Unable to connect")
				return nil
			}

			gm := gateway.NewManager(convdomain.SessionID("status"), cfg, nil)
			version := gm.Version(cmd.Context())
			if version == "" {
				version = "unknown"
			}

			modelCount := len(modelsResp.Data)

			fmt.Printf("Gateway URL: %s\n", cfg.Gateway.URL)
			fmt.Println("Gateway Status: Running")
			fmt.Printf("Version: %s\n", version)
			fmt.Printf("Source: %s\n", gatewaySource(cfg))
			fmt.Printf("Models: %d active\n", modelCount)

			if format != "text" {
				fmt.Printf("Output format: %s\n", format)
			}

			return nil
		},
	}
	command.Flags().StringP("format", "f", "text", "Output format (text, json, yaml)")
	return command
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
		logger.Error("listModels API call failed", "error", err)
		return nil, err
	}

	return &struct {
		Data []string `json:"data"`
	}{
		Data: models,
	}, nil
}

// gatewaySource describes which mode the CLI would manage the gateway in.
func gatewaySource(cfg *config.Config) string {
	if !cfg.Gateway.Run {
		return "external"
	}
	if cfg.Gateway.StandaloneBinary || cfg.Gateway.OCI == "" {
		return "managed binary"
	}
	return "managed container"
}
