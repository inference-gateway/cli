package status

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"slices"
	"time"

	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"

	output "github.com/inference-gateway/cli/cmd/output"
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
			format, _ := cmd.Flags().GetString("format")
			cfg := &config.Config{Gateway: state.Config().Gateway}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), cfg, format)
		},
	}
	output.AddFormatFlag(command, "text", "json", "yaml")
	return command
}

// gatewayStatus is the machine-readable projection --format json|yaml emit.
type gatewayStatus struct {
	URL     string `json:"url" yaml:"url"`
	Status  string `json:"status" yaml:"status"`
	Version string `json:"version" yaml:"version"`
	Source  string `json:"source,omitempty" yaml:"source,omitempty"`
	Models  int    `json:"models" yaml:"models"`
	Error   string `json:"error,omitempty" yaml:"error,omitempty"`
}

func runStatus(ctx context.Context, w io.Writer, cfg *config.Config, format string) error {
	if !slices.Contains([]string{"text", "json", "yaml"}, format) {
		return fmt.Errorf("unknown output format %q (choose from text, json, yaml)", format)
	}
	if format == "text" {
		fmt.Fprintln(w, "Checking inference gateway status...")
	}

	status := probeGateway(ctx, cfg)

	switch format {
	case "json":
		return output.PrintJSON(w, status)
	case "yaml":
		out, err := yaml.Marshal(status)
		if err != nil {
			return fmt.Errorf("failed to marshal status: %w", err)
		}
		fmt.Fprint(w, string(out))
		return nil
	default:
		printGatewayStatusText(w, status)
		return nil
	}
}

// probeGateway asks the gateway for its models and version. An unreachable
// gateway is a status to report, not an error.
func probeGateway(ctx context.Context, cfg *config.Config) gatewayStatus {
	modelsResp, err := fetchModels(cfg)
	if err != nil {
		logger.Warn("gateway unreachable", "error", err)
		return gatewayStatus{URL: cfg.Gateway.URL, Status: "unreachable", Version: "unknown", Error: err.Error()}
	}

	version := gateway.NewSupervisor(convdomain.SessionID("status"), cfg, nil).Version(ctx)
	return gatewayStatus{
		URL:     cfg.Gateway.URL,
		Status:  "running",
		Version: cmp.Or(version, "unknown"),
		Source:  gatewaySource(cfg),
		Models:  len(modelsResp.Data),
	}
}

func printGatewayStatusText(w io.Writer, s gatewayStatus) {
	fmt.Fprintf(w, "Gateway URL: %s\n", s.URL)
	if s.Error != "" {
		fmt.Fprintf(w, "Gateway Status: Unreachable (%s)\n", s.Error)
		fmt.Fprintf(w, "Version: %s\n", s.Version)
		fmt.Fprintln(w, "Models: Unable to connect")
		return
	}
	fmt.Fprintln(w, "Gateway Status: Running")
	fmt.Fprintf(w, "Version: %s\n", s.Version)
	fmt.Fprintf(w, "Source: %s\n", s.Source)
	fmt.Fprintf(w, "Models: %d active\n", s.Models)
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
