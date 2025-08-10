package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

var (
	modelName string
)

var promptCmd = &cobra.Command{
	Use:   "prompt [text]",
	Short: "Send a prompt to the inference gateway",
	Long: `Send a text prompt to the inference gateway for processing.
The prompt text can be provided as arguments or as a single quoted string.

Examples:
  infer prompt --model openai/gpt-4o "What is machine learning?"
  infer prompt --model deepseek/deepseek-chat "Classify this text: This is a great product!"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := strings.Join(args, " ")

		configPath, _ := cmd.Flags().GetString("config")
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if modelName == "" {
			return fmt.Errorf("model name is required. Use --model flag to specify the model")
		}

		client := sdk.NewClient(&sdk.ClientOptions{
			BaseURL: cfg.Gateway.URL + "/v1",
			APIKey:  cfg.Gateway.APIKey,
		})

		verbose, _ := cmd.Flags().GetBool("verbose")

		if verbose {
			fmt.Printf("Sending prompt to model '%s': %s\n", modelName, prompt)
		}

		ctx := context.Background()
		messages := []sdk.Message{
			{Role: sdk.User, Content: prompt},
		}

		provider, actualModel, err := parseProvider(modelName)
		if err != nil {
			return fmt.Errorf("failed to parse provider: %w", err)
		}
		response, err := client.GenerateContent(ctx, provider, actualModel, messages)
		if err != nil {
			return fmt.Errorf("failed to send prompt: %w", err)
		}

		outputFormat := cfg.Output.Format
		if format, _ := cmd.Flags().GetString("format"); format != "" {
			outputFormat = format
		}

		switch outputFormat {
		case "json":
			fmt.Printf("{\"model\": \"%s\", \"prompt\": \"%s\", \"response\": \"%s\"}\n",
				modelName, prompt, response.Choices[0].Message.Content)
		case "yaml":
			fmt.Printf("model: %s\nprompt: %s\nresponse: %s\n",
				modelName, prompt, response.Choices[0].Message.Content)
		default:
			if !cfg.Output.Quiet {
				fmt.Printf("Response from %s:\n", modelName)
			}
			fmt.Println(response.Choices[0].Message.Content)
		}

		return nil
	},
}

func parseProvider(model string) (sdk.Provider, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("model '%s' must be in format 'provider/model', e.g., 'openai/gpt-4' or 'deepseek/deepseek-chat'", model)
	}

	providerStr := strings.ToLower(parts[0])
	modelName := parts[1]

	switch sdk.Provider(providerStr) {
	case sdk.Anthropic:
		return sdk.Anthropic, modelName, nil
	case sdk.Cloudflare:
		return sdk.Cloudflare, modelName, nil
	case sdk.Cohere:
		return sdk.Cohere, modelName, nil
	case sdk.Deepseek:
		return sdk.Deepseek, modelName, nil
	case sdk.Google:
		return sdk.Google, modelName, nil
	case sdk.Groq:
		return sdk.Groq, modelName, nil
	case sdk.Ollama:
		return sdk.Ollama, modelName, nil
	case sdk.Openai:
		return sdk.Openai, modelName, nil
	default:
		return "", "", fmt.Errorf("unsupported provider '%s'. Supported providers: anthropic, cloudflare, cohere, deepseek, google, groq, ollama, openai", providerStr)
	}
}

func init() {
	rootCmd.AddCommand(promptCmd)

	promptCmd.Flags().StringVarP(&modelName, "model", "m", "", "Model name to use for inference (required)")

	promptCmd.Flags().StringP("format", "f", "", "Output format (text, json, yaml)")

	_ = promptCmd.MarkFlagRequired("model")
}
