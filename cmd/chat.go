package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal"
	sdk "github.com/inference-gateway/sdk"
	"github.com/spf13/cobra"
)

// Use SDK types for messages
type ChatMessage = sdk.Message

// ConversationEntry tracks a message along with which model generated it (for assistant messages)
type ConversationEntry struct {
	Message sdk.Message `json:"message"`
	Model   string       `json:"model,omitempty"` // Only set for assistant messages
}

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
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	selectedModel, err := selectModel(models)
	if err != nil {
		return fmt.Errorf("model selection failed: %w", err)
	}

	var conversation []ConversationEntry

	inputModel := internal.NewChatManagerModel()
	inputModel.GetChatInput().SetSelectedModel(selectedModel)
	program := tea.NewProgram(inputModel, tea.WithAltScreen())

	var toolsManager *internal.LLMToolsManager
	if cfg.Tools.Enabled {
		toolsManager = internal.NewLLMToolsManagerWithUI(cfg, program, inputModel.GetChatInput())
	}

	welcomeHistory := []string{
		fmt.Sprintf("🤖 Chat session started with %s", selectedModel),
		"💡 Type '/help' or '?' for commands • Press @ to select files to reference",
	}

	if cfg.Tools.Enabled {
		toolCount := len(createSDKTools(cfg))
		if toolCount > 0 {
			welcomeHistory = append(welcomeHistory, fmt.Sprintf("🔧 %d tool(s) available for the model to use", toolCount))
		}
	}

	welcomeHistory = append(welcomeHistory, "")

	updateHistory := func(conversation []ConversationEntry) {
		if len(conversation) > 0 {
			chatHistory := formatConversationForDisplay(conversation, selectedModel)
			program.Send(internal.UpdateHistoryMsg{History: append(welcomeHistory, chatHistory...)})
		} else {
			program.Send(internal.UpdateHistoryMsg{History: welcomeHistory})
		}
	}

	go func() {
		_, err := program.Run()
		if err != nil {
			fmt.Printf("Error running chat interface: %v\n", err)
		}
	}()

	updateHistory(conversation)

	for {
		updateHistory(conversation)

		userInput := waitForInput(program, inputModel.GetChatInput())
		if userInput == "" {
			program.Quit()
			fmt.Println("\n👋 Chat session ended!")
			os.Exit(0)
		}

		userInput = strings.TrimSpace(userInput)
		if userInput == "" {
			continue
		}

		if handleChatCommands(userInput, &conversation, &selectedModel, models, program, inputModel) {
			continue
		}

		// Handle interactive file reference if user typed "@"
		userInput, err = handleFileReference(userInput)
		if err != nil {
			fmt.Printf("❌ Error with file selection: %v\n", err)
			continue
		}

		processedInput, err := processFileReferences(userInput)
		if err != nil {
			program.Send(internal.SetStatusMsg{Message: fmt.Sprintf("❌ Error processing file references: %v", err), Spinner: false})
			continue
		}

		conversation = append(conversation, ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.User,
				Content: processedInput,
			},
		})

		userChatHistory := formatConversationForDisplay(conversation, selectedModel)
		program.Send(internal.UpdateHistoryMsg{History: userChatHistory})

		program.Send(internal.SetStatusMsg{Message: "Generating response...", Spinner: true})

		inputModel.ResetCancellation()

		var totalMetrics *ChatMetrics
		maxIterations := 10
		for iteration := 0; iteration < maxIterations; iteration++ {
			if inputModel.IsCancelled() {
				conversation = conversation[:len(conversation)-1]
				chatHistory := formatConversationForDisplay(conversation, selectedModel)
				program.Send(internal.UpdateHistoryMsg{History: chatHistory})
				program.Send(internal.SetStatusMsg{Message: "❌ Generation cancelled by user", Spinner: false})
				break
			}

			// Convert conversation to sdk.Message for API call
			sdkMessages := make([]sdk.Message, len(conversation))
			for i, entry := range conversation {
				sdkMessages[i] = entry.Message
			}
			
			_, assistantToolCalls, metrics, err := sendStreamingChatCompletionToUI(cfg, selectedModel, sdkMessages, program, &conversation, inputModel.GetChatInput())

			if err != nil {
				if strings.Contains(err.Error(), "cancelled by user") {
					break
				}
				conversation = conversation[:len(conversation)-1]
				chatHistory := formatConversationForDisplay(conversation, selectedModel)
				program.Send(internal.UpdateHistoryMsg{History: chatHistory})
				program.Send(internal.SetStatusMsg{Message: fmt.Sprintf("❌ Error: %v", err), Spinner: false})
				break
			}

			if totalMetrics == nil {
				totalMetrics = metrics
			} else if metrics != nil {
				totalMetrics.Duration += metrics.Duration
				if totalMetrics.Usage != nil && metrics.Usage != nil {
					totalMetrics.Usage.PromptTokens += metrics.Usage.PromptTokens
					totalMetrics.Usage.CompletionTokens += metrics.Usage.CompletionTokens
					totalMetrics.Usage.TotalTokens += metrics.Usage.TotalTokens
				}
			}

			if len(assistantToolCalls) > 0 && len(conversation) > 0 {
				lastIdx := len(conversation) - 1
				if conversation[lastIdx].Message.Role == sdk.Assistant {
					conversation[lastIdx].Message.ToolCalls = &assistantToolCalls
					chatHistory := formatConversationForDisplay(conversation, selectedModel)
					program.Send(internal.UpdateHistoryMsg{History: chatHistory})
				}
			}

			if len(assistantToolCalls) == 0 {
				break
			}

			toolExecutionFailed := false
			for _, toolCall := range assistantToolCalls {
				if inputModel.IsCancelled() {
					conversation = conversation[:len(conversation)-1]
					chatHistory := formatConversationForDisplay(conversation, selectedModel)
					program.Send(internal.UpdateHistoryMsg{History: chatHistory})
					program.Send(internal.SetStatusMsg{Message: "❌ Generation cancelled by user", Spinner: false})
					toolExecutionFailed = true
					break
				}

				toolResult, err := executeToolCall(toolsManager, toolCall.Function.Name, toolCall.Function.Arguments)
				if err != nil {
					program.Send(internal.SetStatusMsg{Message: fmt.Sprintf("❌ Tool execution failed: %v", err), Spinner: false})
					toolExecutionFailed = true
					break
				} else {
					conversation = append(conversation, ConversationEntry{
						Message: sdk.Message{
							Role:       sdk.Tool,
							Content:    toolResult,
							ToolCallId: &toolCall.Id,
						},
					})
					program.Send(internal.SetStatusMsg{Message: "✅ Tool executed successfully", Spinner: false})
				}
			}

			if toolExecutionFailed {
				conversation = conversation[:len(conversation)-1]
				chatHistory := formatConversationForDisplay(conversation, selectedModel)
				program.Send(internal.UpdateHistoryMsg{History: chatHistory})
				program.Send(internal.SetStatusMsg{Message: "❌ Tool execution was cancelled. Please try a different request.", Spinner: false})
				break
			}

		}

		if totalMetrics != nil {
			metricsMsg := formatMetricsString(totalMetrics)
			program.Send(internal.SetStatusMsg{Message: fmt.Sprintf("✅ Complete - %s", metricsMsg), Spinner: false})
		} else {
			program.Send(internal.SetStatusMsg{Message: "✅ Response complete", Spinner: false})
		}
	}
}

// waitForInput waits for user input from the chat interface
func waitForInput(program *tea.Program, inputModel *internal.ChatInputModel) string {
	for {
		time.Sleep(100 * time.Millisecond)
		if inputModel.HasInput() {
			return inputModel.GetInput()
		}
		if inputModel.IsQuitRequested() {
			return ""
		}
	}
}

func selectModel(models []string) (string, error) {
	modelSelector := internal.NewModelSelectorModel(models)
	program := tea.NewProgram(modelSelector)

	_, err := program.Run()
	if err != nil {
		return "", fmt.Errorf("model selection failed: %w", err)
	}

	if modelSelector.IsCancelled() {
		return "", fmt.Errorf("model selection was cancelled")
	}

	if !modelSelector.IsSelected() {
		return "", fmt.Errorf("no model was selected")
	}

	return modelSelector.GetSelected(), nil
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

func handleChatCommands(input string, conversation *[]ConversationEntry, selectedModel *string, availableModels []string, program *tea.Program, inputModel *internal.ChatManagerModel) bool {
	switch input {
	case "/exit", "/quit":
		os.Exit(0)
		return true
	case "/clear":
		*conversation = []ConversationEntry{}
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
			inputModel.GetChatInput().SetSelectedModel(newModel)
			fmt.Printf("Switched to model: %s\n", newModel)
		}
		return true
	case "/compact":
		err := compactConversation(*conversation, *selectedModel)
		if err != nil {
			fmt.Printf("❌ Error creating compact file: %v\n", err)
		}
		return true
	case "/help", "?":
		showHelpScreen()
		return true
	}

	if strings.HasPrefix(input, "/") {
		fmt.Printf("Unknown command: %s. Type '/help' for available commands.\n", input)
		return true
	}

	return false
}

func showConversationHistory(conversation []ConversationEntry) {
	fmt.Println("💬 Conversation History:")
	if len(conversation) == 0 {
		fmt.Println("  (empty)")
		return
	}

	for i, entry := range conversation {
		var role string
		switch entry.Message.Role {
		case sdk.User:
			role = "You"
		case sdk.Assistant:
			if entry.Model != "" {
				role = fmt.Sprintf("Assistant (%s)", entry.Model)
			} else {
				role = "Assistant"
			}
		case sdk.System:
			role = "System"
		case sdk.Tool:
			role = "Tool"
		default:
			role = string(entry.Message.Role)
		}
		fmt.Printf("  %d. %s: %s\n", i+1, role, entry.Message.Content)
	}
	fmt.Println()
}

func showHelpScreen() {
	helpViewer := internal.NewHelpViewerModel()
	program := tea.NewProgram(helpViewer, tea.WithAltScreen())

	_, err := program.Run()
	if err != nil {
		fmt.Printf("Error displaying help: %v\n", err)
	}
}

// ChatMetrics holds timing and token usage information
type ChatMetrics struct {
	Duration time.Duration
	Usage    *sdk.CompletionUsage
}

func executeToolCall(manager *internal.LLMToolsManager, toolName, arguments string) (string, error) {
	if manager == nil {
		return "", fmt.Errorf("tools are not enabled")
	}

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

type uiStreamingResult struct {
	fullMessage     *strings.Builder
	firstContent    bool
	usage           *sdk.CompletionUsage
	toolCalls       []sdk.ChatCompletionMessageToolCall
	activeToolCalls map[int]*sdk.ChatCompletionMessageToolCall
	program         *tea.Program
	conversation    *[]ConversationEntry
	cfg             *config.Config
	inputModel      *internal.ChatInputModel
	selectedModel   string
}

func createStreamingClient(cfg *config.Config) (sdk.Client, error) {
	baseURL := strings.TrimSuffix(cfg.Gateway.URL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	client := sdk.NewClient(&sdk.ClientOptions{
		BaseURL: baseURL,
		APIKey:  cfg.Gateway.APIKey,
	})

	tools := createSDKTools(cfg)
	if len(tools) > 0 {
		client = client.WithTools(&tools)
	}

	return client, nil
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

func formatMetricsString(metrics *ChatMetrics) string {
	if metrics == nil {
		return ""
	}

	var parts []string

	duration := metrics.Duration.Round(time.Millisecond)
	parts = append(parts, fmt.Sprintf("Time: %v", duration))

	if metrics.Usage != nil {
		if metrics.Usage.PromptTokens > 0 {
			parts = append(parts, fmt.Sprintf("Input: %d tokens", metrics.Usage.PromptTokens))
		}
		if metrics.Usage.CompletionTokens > 0 {
			parts = append(parts, fmt.Sprintf("Output: %d tokens", metrics.Usage.CompletionTokens))
		}
		if metrics.Usage.TotalTokens > 0 {
			parts = append(parts, fmt.Sprintf("Total: %d tokens", metrics.Usage.TotalTokens))
		}
	}

	return strings.Join(parts, " | ")
}

func sendStreamingChatCompletionToUI(cfg *config.Config, model string, messages []ChatMessage, program *tea.Program, conversation *[]ConversationEntry, inputModel *internal.ChatInputModel) (string, []sdk.ChatCompletionMessageToolCall, *ChatMetrics, error) {
	client, err := createStreamingClient(cfg)
	if err != nil {
		return "", nil, nil, err
	}

	ctx := context.Background()
	startTime := time.Now()

	provider, modelName, err := parseProvider(model)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to parse provider from model '%s': %w", model, err)
	}

	events, err := client.GenerateContentStream(ctx, provider, modelName, messages)
	if err != nil {
		return "", nil, nil, fmt.Errorf("failed to generate content stream: %w", err)
	}

	result := &uiStreamingResult{
		fullMessage:     &strings.Builder{},
		firstContent:    true,
		activeToolCalls: make(map[int]*sdk.ChatCompletionMessageToolCall),
		program:         program,
		conversation:    conversation,
		cfg:             cfg,
		inputModel:      inputModel,
		selectedModel:   model,
	}

	err = processStreamingEventsToUI(events, result)
	if err != nil {
		return "", nil, nil, err
	}

	finalToolCalls := result.toolCalls

	duration := time.Since(startTime)
	metrics := &ChatMetrics{
		Duration: duration,
		Usage:    result.usage,
	}

	return result.fullMessage.String(), finalToolCalls, metrics, nil
}

func processStreamingEventsToUI(events <-chan sdk.SSEvent, result *uiStreamingResult) error {
	for event := range events {
		// Check for cancellation
		if result.inputModel.IsCancelled() {
			return fmt.Errorf("generation cancelled by user")
		}

		if event.Event == nil {
			continue
		}

		switch *event.Event {
		case sdk.ContentDelta:
			if err := handleContentDeltaToUI(event, result); err != nil {
				return err
			}
		case sdk.StreamEnd:
			handleStreamEndToUI(result)
			return nil
		case "error":
			return handleStreamErrorToUI(event, result)
		}
	}
	return nil
}

func handleContentDeltaToUI(event sdk.SSEvent, result *uiStreamingResult) error {
	if event.Data == nil {
		return nil
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		return nil
	}

	for _, choice := range streamResponse.Choices {
		handleContentChoiceToUI(choice, result)
		handleToolCallsChoiceToUI(choice, result)
	}

	if streamResponse.Usage != nil {
		result.usage = streamResponse.Usage
		// Update status with token count
		result.program.Send(internal.SetStatusMsg{
			Message: fmt.Sprintf("Generating response... (%d tokens)", streamResponse.Usage.TotalTokens),
			Spinner: true,
		})
	}

	return nil
}

func handleContentChoiceToUI(choice sdk.ChatCompletionStreamChoice, result *uiStreamingResult) {
	if choice.Delta.Content == "" {
		return
	}

	if result.firstContent {
		// Add assistant message to conversation and update UI
		assistantEntry := ConversationEntry{
			Message: sdk.Message{
				Role:    sdk.Assistant,
				Content: choice.Delta.Content,
			},
			Model: result.selectedModel,
		}
		*result.conversation = append(*result.conversation, assistantEntry)

		// Update UI with new history using model name
		chatHistory := formatConversationForDisplay(*result.conversation, result.selectedModel)
		result.program.Send(internal.UpdateHistoryMsg{History: chatHistory})

		result.firstContent = false
	} else {
		// Update the last message in conversation
		if len(*result.conversation) > 0 {
			lastIdx := len(*result.conversation) - 1
			(*result.conversation)[lastIdx].Message.Content += choice.Delta.Content

			// Update UI with updated history using model name
			chatHistory := formatConversationForDisplay(*result.conversation, result.selectedModel)
			result.program.Send(internal.UpdateHistoryMsg{History: chatHistory})
		}
	}

	result.fullMessage.WriteString(choice.Delta.Content)
}

func handleToolCallsChoiceToUI(choice sdk.ChatCompletionStreamChoice, result *uiStreamingResult) {
	if len(choice.Delta.ToolCalls) == 0 {
		return
	}

	for _, deltaToolCall := range choice.Delta.ToolCalls {
		handleToolCallDeltaToUI(deltaToolCall, result)
	}
}

func handleToolCallDeltaToUI(deltaToolCall sdk.ChatCompletionMessageToolCallChunk, result *uiStreamingResult) {
	index := deltaToolCall.Index

	if result.activeToolCalls[index] == nil {
		// Ensure we have an assistant message in the conversation when tool calls start
		if result.firstContent {
			// Add empty assistant message to conversation since tool calls are starting
			assistantEntry := ConversationEntry{
				Message: sdk.Message{
					Role:    sdk.Assistant,
					Content: "",
				},
				Model: result.selectedModel,
			}
			*result.conversation = append(*result.conversation, assistantEntry)
			result.firstContent = false
		}

		result.activeToolCalls[index] = &sdk.ChatCompletionMessageToolCall{
			Id:   deltaToolCall.ID,
			Type: sdk.ChatCompletionToolType(deltaToolCall.Type),
			Function: sdk.ChatCompletionMessageToolCallFunction{
				Name:      deltaToolCall.Function.Name,
				Arguments: "",
			},
		}
		if deltaToolCall.Function.Name != "" {
			result.program.Send(internal.SetStatusMsg{
				Message: fmt.Sprintf("🔧 Calling tool: %s", deltaToolCall.Function.Name),
				Spinner: true,
			})
		}
	}

	if deltaToolCall.Function.Arguments != "" {
		result.activeToolCalls[index].Function.Arguments += deltaToolCall.Function.Arguments
	}
}

func handleStreamEndToUI(result *uiStreamingResult) {
	var toolCalls []sdk.ChatCompletionMessageToolCall
	for _, toolCall := range result.activeToolCalls {
		if toolCall.Function.Name != "" {
			result.program.Send(internal.SetStatusMsg{
				Message: fmt.Sprintf("🔧 Tool: %s with arguments: %s", toolCall.Function.Name, toolCall.Function.Arguments),
				Spinner: false,
			})
			toolCalls = append(toolCalls, *toolCall)
		}
	}
	result.toolCalls = toolCalls
}

func handleStreamErrorToUI(event sdk.SSEvent, result *uiStreamingResult) error {
	if event.Data == nil {
		return fmt.Errorf("stream error: unknown error")
	}

	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(*event.Data, &errResp); err != nil {
		return fmt.Errorf("stream error: failed to parse error response")
	}
	return fmt.Errorf("stream error: %s", errResp.Error)
}

// scanProjectFiles recursively scans the current directory for files,
// excluding common directories that should not be included
func scanProjectFiles() ([]string, error) {
	var files []string

	// Directories to exclude from scanning
	excludeDirs := map[string]bool{
		".git":         true,
		".github":      true,
		"node_modules": true,
		".infer":       true,
		"vendor":       true,
		".flox":        true,
		"dist":         true,
		"build":        true,
		"bin":          true,
		".vscode":      true,
		".idea":        true,
	}

	// File extensions to exclude
	excludeExts := map[string]bool{
		".exe":   true,
		".bin":   true,
		".dll":   true,
		".so":    true,
		".dylib": true,
		".a":     true,
		".o":     true,
		".obj":   true,
		".pyc":   true,
		".class": true,
		".jar":   true,
		".war":   true,
		".zip":   true,
		".tar":   true,
		".gz":    true,
		".rar":   true,
		".7z":    true,
		".png":   true,
		".jpg":   true,
		".jpeg":  true,
		".gif":   true,
		".bmp":   true,
		".ico":   true,
		".svg":   true,
		".pdf":   true,
		".mov":   true,
		".mp4":   true,
		".avi":   true,
		".mp3":   true,
		".wav":   true,
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current directory: %w", err)
	}

	err = filepath.WalkDir(cwd, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}

		// Get relative path from current directory
		relPath, err := filepath.Rel(cwd, path)
		if err != nil {
			return nil // Skip if we can't get relative path
		}

		// Skip directories that should be excluded
		if d.IsDir() {
			if excludeDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip files with excluded extensions
		ext := strings.ToLower(filepath.Ext(relPath))
		if excludeExts[ext] {
			return nil
		}

		// Skip very large files (over 1MB)
		if info, err := d.Info(); err == nil && info.Size() > 1024*1024 {
			return nil
		}

		// Only include regular files
		if d.Type().IsRegular() {
			files = append(files, relPath)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	// Sort files for consistent ordering
	sort.Strings(files)

	return files, nil
}

// selectFileInteractively shows a dropdown to select a file from the project
func selectFileInteractively() (string, error) {
	files, err := scanProjectFiles()
	if err != nil {
		return "", fmt.Errorf("failed to scan project files: %w", err)
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no files found in the current directory")
	}

	// Add a limit to prevent overwhelming dropdown
	maxFiles := 200
	if len(files) > maxFiles {
		files = files[:maxFiles]
		fmt.Printf("⚠️  Showing first %d files (found %d total)\n", maxFiles, len(files))
	}

	fileSelector := internal.NewFileSelectorModel(files)
	program := tea.NewProgram(fileSelector)

	_, err = program.Run()
	if err != nil {
		return "", fmt.Errorf("file selection failed: %w", err)
	}

	if fileSelector.IsCancelled() {
		return "", fmt.Errorf("file selection cancelled")
	}

	if !fileSelector.IsSelected() {
		return "", fmt.Errorf("no file was selected")
	}

	return fileSelector.GetSelected(), nil
}

// handleFileReference processes "@" references in user input
func handleFileReference(input string) (string, error) {
	if strings.TrimSpace(input) == "@" {
		selectedFile, err := selectFileInteractively()
		if err != nil {
			return "", err
		}
		return "@" + selectedFile, nil
	}

	if strings.HasSuffix(strings.TrimSpace(input), "@") {
		selectedFile, err := selectFileInteractively()
		if err != nil {
			return "", err
		}

		trimmed := strings.TrimSpace(input)
		prefix := trimmed[:len(trimmed)-1]
		return prefix + "@" + selectedFile, nil
	}

	return input, nil
}

func compactConversation(conversation []ConversationEntry, selectedModel string) error {
	cfg, err := config.LoadConfig("")
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if len(conversation) == 0 {
		fmt.Println("📝 No conversation to export - conversation history is empty")
		return nil
	}

	outputDir := cfg.Compact.OutputDir
	if outputDir == "" {
		outputDir = ".infer"
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory '%s': %w", outputDir, err)
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("chat-export-%s.md", timestamp)
	filePath := filepath.Join(outputDir, filename)

	var content strings.Builder
	content.WriteString("# Chat Session Export\n\n")
	content.WriteString(fmt.Sprintf("**Default Model:** %s\n", selectedModel))
	content.WriteString(fmt.Sprintf("**Date:** %s\n", time.Now().Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("**Total Messages:** %d\n\n", len(conversation)))
	content.WriteString("---\n\n")

	for i, entry := range conversation {
		var role string
		switch entry.Message.Role {
		case sdk.User:
			role = "👤 **You**"
		case sdk.Assistant:
			if entry.Model != "" {
				role = fmt.Sprintf("🤖 **Assistant (%s)**", entry.Model)
			} else {
				role = "🤖 **Assistant**"
			}
		case sdk.System:
			role = "⚙️ **System**"
		case sdk.Tool:
			role = "🔧 **Tool Result**"
		default:
			role = fmt.Sprintf("**%s**", string(entry.Message.Role))
		}

		content.WriteString(fmt.Sprintf("## Message %d - %s\n\n", i+1, role))

		if entry.Message.Content != "" {
			content.WriteString(entry.Message.Content)
			content.WriteString("\n\n")
		}

		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			content.WriteString("### Tool Calls\n\n")
			for _, toolCall := range *entry.Message.ToolCalls {
				content.WriteString(fmt.Sprintf("**Tool:** %s\n\n", toolCall.Function.Name))
				if toolCall.Function.Arguments != "" {
					content.WriteString("**Arguments:**\n```json\n")
					content.WriteString(toolCall.Function.Arguments)
					content.WriteString("\n```\n\n")
				}
			}
		}

		if entry.Message.ToolCallId != nil {
			content.WriteString(fmt.Sprintf("*Tool Call ID: %s*\n\n", *entry.Message.ToolCallId))
		}

		content.WriteString("---\n\n")
	}

	content.WriteString(fmt.Sprintf("*Exported on %s using Inference Gateway CLI*\n", time.Now().Format("2006-01-02 15:04:05")))

	if err := os.WriteFile(filePath, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write file '%s': %w", filePath, err)
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	fmt.Printf("📝 Conversation exported successfully to: %s\n", absPath)
	fmt.Printf("📊 Exported %d message(s) from chat session with %s\n", len(conversation), selectedModel)

	return nil
}

func formatConversationForDisplay(conversation []ConversationEntry, selectedModel string) []string {
	var history []string

	for _, entry := range conversation {
		var role string
		var content string

		switch entry.Message.Role {
		case sdk.User:
			role = "👤 You"
		case sdk.Assistant:
			if entry.Model != "" {
				role = fmt.Sprintf("🤖 %s", entry.Model)
			} else {
				role = fmt.Sprintf("🤖 %s", selectedModel)
			}
		case sdk.System:
			role = "⚙️ System"
		case sdk.Tool:
			role = "🔧 Tool"
		default:
			role = string(entry.Message.Role)
		}

		content = entry.Message.Content

		content = strings.ReplaceAll(content, "\r\n", "\n")
		content = strings.ReplaceAll(content, "\r", "\n")

		if content != "" {
			history = append(history, fmt.Sprintf("%s: %s", role, content))
		}

		if entry.Message.ToolCalls != nil && len(*entry.Message.ToolCalls) > 0 {
			for _, toolCall := range *entry.Message.ToolCalls {
				history = append(history, fmt.Sprintf("🔧 Tool Call: %s", toolCall.Function.Name))
			}
		}
	}

	return history
}

func init() {
	rootCmd.AddCommand(chatCmd)
}
