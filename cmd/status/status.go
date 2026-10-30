package status

import (
	"context"
	"fmt"
	"io"
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
			return runStatus(cmd.OutOrStdout(), state, cmd)
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

func runStatus(w io.Writer, state *runtime.State, cmd *cobra.Command) error {
	format, _ := cmd.Flags().GetString("format")

	loaded := state.Config()

	cfg := &config.Config{
		Gateway: loaded.Gateway,
	}

	if format == "text" {
		fmt.Fprintln(w, "Checking inference gateway status...")
	}

	modelsResp, err := fetchModels(cfg)
	if err != nil {
		logger.Warn("gateway unreachable", "error", err)
		s := gatewayStatus{
			URL:     cfg.Gateway.URL,
			Status:  "unreachable",
			Version: "unknown",
			Models:  0,
			Error:   err.Error(),
		}
		if format == "text" {
			fmt.Fprintf(w, "Gateway URL: %s\n", s.URL)
			fmt.Fprintf(w, "Gateway Status: Unreachable (%v)\n", err)
			fmt.Fprintln(w, "Version: unknown")
			fmt.Fprintln(w, "Models: Unable to connect")
			return nil
		}
		return emitGatewayStatus(w, format, s)
	}

	gm := gateway.NewSupervisor(convdomain.SessionID("status"), cfg, nil)
	version := gm.Version(cmd.Context())
	if version == "" {
		version = "unknown"
	}

	s := gatewayStatus{
		URL:     cfg.Gateway.URL,
		Status:  "running",
		Version: version,
		Source:  gatewaySource(cfg),
		Models:  len(modelsResp.Data),
	}
	if format == "text" {
		fmt.Fprintf(w, "Gateway URL: %s\n", s.URL)
		fmt.Fprintln(w, "Gateway Status: Running")
		fmt.Fprintf(w, "Version: %s\n", s.Version)
		fmt.Fprintf(w, "Source: %s\n", s.Source)
		fmt.Fprintf(w, "Models: %d active\n", s.Models)
		return nil
	}
	return emitGatewayStatus(w, format, s)
}

// emitGatewayStatus renders the status projection for the json or yaml formats.
func emitGatewayStatus(w io.Writer, format string, s gatewayStatus) error {
	switch format {
	case "json":
		return output.PrintJSON(w, s)
	case "yaml":
		out, err := yaml.Marshal(s)
		if err != nil {
			return fmt.Errorf("failed to marshal status: %w", err)
		}
		fmt.Fprint(w, string(out))
		return nil
	default:
		return fmt.Errorf("unknown output format %q (choose from text, json, yaml)", format)
	}
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
