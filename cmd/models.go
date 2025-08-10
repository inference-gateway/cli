package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/spf13/cobra"
)

// Model represents a model returned from the inference gateway
type Model struct {
	ID       string            `json:"id"`
	Object   string            `json:"object,omitempty"`
	Created  int64             `json:"created,omitempty"`
	OwnedBy  string            `json:"owned_by,omitempty"`
	Root     string            `json:"root,omitempty"`
	Parent   string            `json:"parent,omitempty"`
	MetaData map[string]string `json:"metadata,omitempty"`
}

// ModelsResponse represents the response from /v1/models endpoint
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage and list available models",
	Long: `Manage and list available models on the inference gateway.
This command allows you to view all models deployed on the gateway.`,
}

var listModelsCmd = &cobra.Command{
	Use:   "list",
	Short: "List all available models",
	Long: `List all models available on the inference gateway.
Connects to the gateway and fetches the current list of deployed models.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")
		format, _ := cmd.Flags().GetString("format")

		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		models, err := fetchModels(cfg)
		if err != nil {
			return fmt.Errorf("failed to fetch models: %w", err)
		}

		return displayModels(models, format)
	},
}

// fetchModels retrieves the list of models from the inference gateway
func fetchModels(cfg *config.Config) (*ModelsResponse, error) {
	url := strings.TrimSuffix(cfg.Gateway.URL, "/") + "/v1/models"

	client := &http.Client{
		Timeout: time.Duration(cfg.Gateway.Timeout) * time.Second,
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if cfg.Gateway.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Gateway.APIKey)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "inference-gateway-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to %s: %w", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gateway returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var modelsResp ModelsResponse
	if err := json.Unmarshal(body, &modelsResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &modelsResp, nil
}

// displayModels outputs the models in the specified format
func displayModels(modelsResp *ModelsResponse, format string) error {
	switch strings.ToLower(format) {
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(modelsResp)
	case "yaml":
		fallthrough
	default:
		if len(modelsResp.Data) == 0 {
			fmt.Println("No models found on the inference gateway")
			return nil
		}

		fmt.Printf("Available Models (%d total):\n\n", len(modelsResp.Data))

		for i, model := range modelsResp.Data {
			fmt.Printf("%d. %s", i+1, model.ID)

			if model.OwnedBy != "" {
				fmt.Printf(" (owned by: %s)", model.OwnedBy)
			}

			if model.Created > 0 {
				created := time.Unix(model.Created, 0)
				fmt.Printf(" [created: %s]", created.Format("2006-01-02 15:04:05"))
			}

			fmt.Println()

			if model.Root != "" && model.Root != model.ID {
				fmt.Printf("   Root: %s\n", model.Root)
			}

			if model.Parent != "" {
				fmt.Printf("   Parent: %s\n", model.Parent)
			}

			if len(model.MetaData) > 0 {
				fmt.Printf("   Metadata:\n")
				for key, value := range model.MetaData {
					fmt.Printf("     %s: %s\n", key, value)
				}
			}

			if i < len(modelsResp.Data)-1 {
				fmt.Println()
			}
		}
	}

	return nil
}

func init() {
	rootCmd.AddCommand(modelsCmd)

	modelsCmd.AddCommand(listModelsCmd)

	listModelsCmd.Flags().StringP("format", "f", "text", "Output format (text, json)")
}
