package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal"
	sdk "github.com/inference-gateway/sdk"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

// Use SDK types for messages
type ChatMessage = sdk.Message

// ChatRequest represents a chat completion request
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
}

// ChatResponse represents a chat completion response
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Start an interactive chat session with model selection",
	Long: `Start an interactive chat session where you can select a model from a dropdown
and have a conversational interface with the inference gateway.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return startChatSession()
	},
}

func startChatSession() error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	models, err := getAvailableModelsList(cfg)
	if err != nil {
		fmt.Printf("Warning: Could not fetch models from gateway: %v\n", err)
		fmt.Println("Using fallback models...")
		models = []string{"gpt-4", "gpt-3.5-turbo", "claude-3-sonnet", "claude-3-haiku"}
	}

	selectedModel, err := selectModel(models)
	if err != nil {
		return fmt.Errorf("model selection failed: %w", err)
	}

	fmt.Printf("\n🤖 Starting chat session with %s\n", selectedModel)
	fmt.Println("Commands: '/exit' to quit, '/clear' for history, '/switch' for models, '/help' for all")
	fmt.Println("Commands are processed immediately and won't be sent to the model")
	fmt.Println("📁 File references: Use @filename to include file contents in your message")

	if cfg.Tools.Enabled {
		toolCount := len(createSDKTools(cfg))
		if toolCount > 0 {
			fmt.Printf("🔧 Tools enabled: %d tool(s) available for the model to use\n", toolCount)
		}
	}

	var conversation []sdk.Message

	for {
		prompt := promptui.Prompt{
			Label:       "You",
			HideEntered: false,
			Templates: &promptui.PromptTemplates{
				Prompt:  "{{ . | bold }}{{ \":\" | faint }} ",
				Valid:   "{{ . | bold }}{{ \":\" | faint }} ",
				Invalid: "{{ . | bold }}{{ \":\" | faint }} ",
			},
		}

		userInput, err := prompt.Run()
		if err != nil {
			if err == promptui.ErrInterrupt {
				break
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		userInput = strings.TrimSpace(userInput)
		if userInput == "" {
			continue
		}

		if handleChatCommands(userInput, &conversation, &selectedModel, models) {
			continue
		}

		processedInput, err := processFileReferences(userInput)
		if err != nil {
			fmt.Printf("❌ Error processing file references: %v\n", err)
			continue
		}

		conversation = append(conversation, sdk.Message{
			Role:    sdk.User,
			Content: processedInput,
		})

		fmt.Printf("\n%s: ", selectedModel)

		var wg sync.WaitGroup
		var spinnerActive = true
		var mu sync.Mutex

		wg.Add(1)
		go func() {
			defer wg.Done()
			showSpinner(&spinnerActive, &mu)
		}()

		assistantMessage, assistantToolCalls, metrics, err := sendStreamingChatCompletion(cfg, selectedModel, conversation, &spinnerActive, &mu)

		wg.Wait()

		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			conversation = conversation[:len(conversation)-1]
			continue
		}

		conversation = append(conversation, sdk.Message{
			Role:      sdk.Assistant,
			Content:   assistantMessage,
			ToolCalls: &assistantToolCalls,
		})

		// If tools were called, we need to add tool result messages to the conversation
		for _, toolCall := range assistantToolCalls {
			// Tool results are already displayed to user during streaming,
			// but we need to add them to conversation history for the model
			toolResult, err := executeToolCall(cfg, toolCall.Function.Name, toolCall.Function.Arguments)
			if err == nil {
				conversation = append(conversation, sdk.Message{
					Role:       sdk.Tool,
					Content:    toolResult,
					ToolCallId: &toolCall.Id,
				})
			}
		}

		displayChatMetrics(metrics)
		fmt.Print("\n\n")
	}

	fmt.Println("\n👋 Chat session ended!")
	return nil
}

func selectModel(models []string) (string, error) {
	searcher := func(input string, index int) bool {
		model := models[index]
		name := strings.ReplaceAll(strings.ToLower(model), " ", "")
		input = strings.ReplaceAll(strings.ToLower(input), " ", "")
		return strings.Contains(name, input)
	}

	prompt := promptui.Select{
		Label:    "Search and select a model for the chat session (type / to search)",
		Items:    models,
		Size:     10,
		Searcher: searcher,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "▶ {{ . | cyan | bold }}",
			Inactive: "  {{ . }}",
			Selected: "✓ Selected model: {{ . | green | bold }}",
		},
	}

	_, result, err := prompt.Run()
	return result, err
}

func getAvailableModelsList(cfg *config.Config) ([]string, error) {
	modelsResp, err := fetchModels(cfg)
	if err != nil {
		return nil, err
	}

	models := make([]string, len(modelsResp.Data))
	for i, model := range modelsResp.Data {
		models[i] = model.ID
	}
	return models, nil
}

func handleChatCommands(input string, conversation *[]sdk.Message, selectedModel *string, availableModels []string) bool {
	switch input {
	case "/exit", "/quit":
		os.Exit(0)
		return true
	case "/clear":
		*conversation = []sdk.Message{}
		fmt.Println("🧹 Conversation history cleared!")
		return true
	case "/history":
		showConversationHistory(*conversation)
		return true
	case "/models":
		fmt.Printf("Current model: %s\n", *selectedModel)
		fmt.Println("Available models:", strings.Join(availableModels, ", "))
		return true
	case "/switch":
		newModel, err := selectModel(availableModels)
		if err != nil {
			fmt.Printf("Error switching model: %v\n", err)
		} else {
			*selectedModel = newModel
			fmt.Printf("Switched to model: %s\n", newModel)
		}
		return true
	case "/help":
		showChatHelp()
		return true
	}

	if strings.HasPrefix(input, "/") {
		fmt.Printf("Unknown command: %s. Type '/help' for available commands.\n", input)
		return true
	}

	return false
}

func showConversationHistory(conversation []sdk.Message) {
	fmt.Println("💬 Conversation History:")
	if len(conversation) == 0 {
		fmt.Println("  (empty)")
		return
	}

	for i, msg := range conversation {
		var role string
		switch msg.Role {
		case sdk.User:
			role = "You"
		case sdk.Assistant:
			role = "Assistant"
		case sdk.System:
			role = "System"
		case sdk.Tool:
			role = "Tool"
		default:
			role = string(msg.Role)
		}
		fmt.Printf("  %d. %s: %s\n", i+1, role, msg.Content)
	}
	fmt.Println()
}

func showChatHelp() {
	fmt.Println("💬 Chat Session Commands:")
	fmt.Println()
	fmt.Println("Chat Commands:")
	fmt.Println("  /exit, /quit     - Exit the chat session")
	fmt.Println("  /clear           - Clear conversation history")
	fmt.Println("  /history         - Show conversation history")
	fmt.Println("  /models          - Show current and available models")
	fmt.Println("  /switch          - Switch to a different model")
	fmt.Println("  /help            - Show this help")
	fmt.Println()
	fmt.Println("File References:")
	fmt.Println("  @filename.txt    - Include contents of filename.txt in your message")
	fmt.Println("  @./config.yaml   - Include contents of config.yaml from current directory")
	fmt.Println("  @../README.md    - Include contents of README.md from parent directory")
	fmt.Println("  Maximum file size: 100KB")
	fmt.Println()
	fmt.Println("Tool Usage:")
	fmt.Println("  Models can invoke available tools automatically during conversation")
	fmt.Println("  Use 'infer tools list' to see whitelisted commands")
	fmt.Println("  Use 'infer tools enable/disable' to control tool access")
	fmt.Println("  Tools execute securely with command whitelisting")
	fmt.Println()
	fmt.Println("Input Tips:")
	fmt.Println("  End line with '\\' for multi-line input")
	fmt.Println("  Press Ctrl+C to interrupt")
	fmt.Println("  Press Ctrl+D to exit")
	fmt.Println()
}

// ChatMetrics holds timing and token usage information
type ChatMetrics struct {
	Duration time.Duration
	Usage    *sdk.CompletionUsage
}

func executeToolCall(cfg *config.Config, toolName, arguments string) (string, error) {
	manager := internal.NewLLMToolsManager(cfg)

	var params map[string]interface{}
	if arguments != "" {
		if err := json.Unmarshal([]byte(arguments), &params); err != nil {
			return "", fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	} else {
		params = make(map[string]interface{})
	}

	return manager.InvokeTool(toolName, params)
}

func createSDKTools(cfg *config.Config) []sdk.ChatCompletionTool {
	if !cfg.Tools.Enabled {
		return nil
	}

	manager := internal.NewLLMToolsManager(cfg)
	toolDefinitions := manager.GetAvailableTools()

	sdkTools := make([]sdk.ChatCompletionTool, len(toolDefinitions))
	for i, toolDef := range toolDefinitions {
		description := toolDef.Description
		sdkTools[i] = sdk.ChatCompletionTool{
			Type: sdk.Function,
			Function: sdk.FunctionObject{
				Name:        toolDef.Name,
				Description: &description,
				Parameters:  (*sdk.FunctionParameters)(&toolDef.Parameters),
			},
		}
	}

	return sdkTools
}

func sendStreamingChatCompletion(cfg *config.Config, model string, messages []ChatMessage, spinnerActive *bool, mu *sync.Mutex) (string, []sdk.ChatCompletionMessageToolCall, *ChatMetrics, error) {
	baseURL := strings.TrimSuffix(cfg.Gateway.URL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	client := sdk.NewClient(&sdk.ClientOptions{
		BaseURL: baseURL,
		APIKey:  cfg.Gateway.APIKey,
	})

	// Add tools if enabled
	tools := createSDKTools(cfg)
	if len(tools) > 0 {
		client = client.WithTools(&tools)
	}

	ctx := context.Background()
	startTime := time.Now()

	// Messages are already SDK types, no conversion needed
	sdkMessages := messages

	provider, modelName, err := parseProvider(model)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	events, err := client.GenerateContentStream(ctx, provider, modelName, sdkMessages)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to generate content stream: %w", err)
	}

	var fullMessage strings.Builder
	var firstContent = true
	var usage *sdk.CompletionUsage
	var toolCalls []sdk.ChatCompletionMessageToolCall
	var activeToolCalls = make(map[int]*sdk.ChatCompletionMessageToolCall)

	for event := range events {
		if event.Event == nil {
			continue
		}

		switch *event.Event {
		case sdk.ContentDelta:
			if event.Data != nil {
				var streamResponse sdk.CreateChatCompletionStreamResponse
				if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
					continue
				}

				for _, choice := range streamResponse.Choices {
					if choice.Delta.Content != "" {
						if firstContent {
							mu.Lock()
							*spinnerActive = false
							mu.Unlock()
							firstContent = false
						}

						fmt.Print(choice.Delta.Content)
						fullMessage.WriteString(choice.Delta.Content)
					}

					// Handle tool calls - accumulate during streaming
					if len(choice.Delta.ToolCalls) > 0 {
						if firstContent {
							mu.Lock()
							*spinnerActive = false
							mu.Unlock()
							firstContent = false
						}

						for _, deltaToolCall := range choice.Delta.ToolCalls {
							index := deltaToolCall.Index

							// Initialize tool call if it doesn't exist
							if activeToolCalls[index] == nil {
								activeToolCalls[index] = &sdk.ChatCompletionMessageToolCall{
									Id:   deltaToolCall.ID,
									Type: sdk.ChatCompletionToolType(deltaToolCall.Type),
									Function: sdk.ChatCompletionMessageToolCallFunction{
										Name:      deltaToolCall.Function.Name,
										Arguments: "",
									},
								}
								if deltaToolCall.Function.Name != "" {
									fmt.Printf("\n🔧 Calling tool: %s", deltaToolCall.Function.Name)
								}
							}

							// Accumulate arguments
							if deltaToolCall.Function.Arguments != "" {
								activeToolCalls[index].Function.Arguments += deltaToolCall.Function.Arguments
							}
						}
					}
				}

				if streamResponse.Usage != nil {
					usage = streamResponse.Usage
				}
			}

		case sdk.StreamEnd:
			mu.Lock()
			*spinnerActive = false
			mu.Unlock()

			// Execute any completed tool calls
			for _, toolCall := range activeToolCalls {
				if toolCall.Function.Name != "" {
					fmt.Printf(" with arguments: %s\n", toolCall.Function.Arguments)

					// Execute the tool call
					toolResult, err := executeToolCall(cfg, toolCall.Function.Name, toolCall.Function.Arguments)
					if err != nil {
						fmt.Printf("❌ Tool execution failed: %v\n", err)
					} else {
						fmt.Printf("✅ Tool result:\n%s\n", toolResult)
					}

					// Store completed tool call for conversation history
					toolCalls = append(toolCalls, *toolCall)
				}
			}

			duration := time.Since(startTime)
			metrics := &ChatMetrics{
				Duration: duration,
				Usage:    usage,
			}
			return fullMessage.String(), toolCalls, metrics, nil

		case "error":
			mu.Lock()
			*spinnerActive = false
			mu.Unlock()
			if event.Data != nil {
				var errResp struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(*event.Data, &errResp); err != nil {
					continue
				}
				return "", nil, nil, fmt.Errorf("stream error: %s", errResp.Error)
			}
		}
	}

	mu.Lock()
	*spinnerActive = false
	mu.Unlock()

	// Execute any remaining tool calls that weren't handled in StreamEnd
	for _, toolCall := range activeToolCalls {
		if toolCall.Function.Name != "" {
			// Check if this tool call was already processed
			alreadyProcessed := false
			for _, processedCall := range toolCalls {
				if processedCall.Id == toolCall.Id {
					alreadyProcessed = true
					break
				}
			}

			if !alreadyProcessed {
				fmt.Printf(" with arguments: %s\n", toolCall.Function.Arguments)

				// Execute the tool call
				toolResult, err := executeToolCall(cfg, toolCall.Function.Name, toolCall.Function.Arguments)
				if err != nil {
					fmt.Printf("❌ Tool execution failed: %v\n", err)
				} else {
					fmt.Printf("✅ Tool result:\n%s\n", toolResult)
				}

				// Store completed tool call for conversation history
				toolCalls = append(toolCalls, *toolCall)
			}
		}
	}

	duration := time.Since(startTime)
	metrics := &ChatMetrics{
		Duration: duration,
		Usage:    usage,
	}
	return fullMessage.String(), toolCalls, metrics, nil
}

func showSpinner(active *bool, mu *sync.Mutex) {
	spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	i := 0

	for {
		mu.Lock()
		if !*active {
			mu.Unlock()
			break
		}
		mu.Unlock()

		fmt.Printf("%s", spinner[i%len(spinner)])
		fmt.Printf("\b")
		i++
		time.Sleep(100 * time.Millisecond)
	}

	fmt.Print(" \b")
}

func processFileReferences(input string) (string, error) {
	fileRefRegex := regexp.MustCompile(`@([\w\./\-_]+(?:\.[\w]+)?)`)
	matches := fileRefRegex.FindAllStringSubmatch(input, -1)

	if len(matches) == 0 {
		return input, nil
	}

	processedInput := input

	for _, match := range matches {
		fullMatch := match[0]
		filePath := match[1]

		content, err := readFileForChat(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read file '%s': %w", filePath, err)
		}

		replacement := fmt.Sprintf("\n\n--- File: %s ---\n%s\n--- End of %s ---\n", filePath, content, filePath)
		processedInput = strings.Replace(processedInput, fullMatch, replacement, 1)
	}

	return processedInput, nil
}

func readFileForChat(filePath string) (string, error) {
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve file path: %w", err)
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist")
	}
	if err != nil {
		return "", fmt.Errorf("failed to access file: %w", err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file")
	}

	const maxFileSize = 100 * 1024
	if info.Size() > maxFileSize {
		return "", fmt.Errorf("file too large (max %d bytes)", maxFileSize)
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

func displayChatMetrics(metrics *ChatMetrics) {
	if metrics == nil {
		return
	}

	fmt.Printf("\n📊 ")

	duration := metrics.Duration.Round(time.Millisecond)
	fmt.Printf("Time: %v", duration)

	if metrics.Usage != nil {
		if metrics.Usage.PromptTokens > 0 {
			fmt.Printf(" | Input: %d tokens", metrics.Usage.PromptTokens)
		}
		if metrics.Usage.CompletionTokens > 0 {
			fmt.Printf(" | Output: %d tokens", metrics.Usage.CompletionTokens)
		}
		if metrics.Usage.TotalTokens > 0 {
			fmt.Printf(" | Total: %d tokens", metrics.Usage.TotalTokens)
		}
	}
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
