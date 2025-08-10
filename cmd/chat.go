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
		return fmt.Errorf("inference gateway is not available: %w", err)
	}

	selectedModel, err := selectModel(models)
	if err != nil {
		return fmt.Errorf("model selection failed: %w", err)
	}

	fmt.Printf("\n🤖 Starting chat session with %s\n", selectedModel)
	fmt.Println("Commands: '/exit' to quit, '/clear' for history, '/compact' to export, '/help' for all")
	fmt.Println("Commands are processed immediately and won't be sent to the model")
	fmt.Println("📁 File references: Type '@' alone to select files interactively, or use @filename directly")

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

		// Handle interactive file reference if user typed "@"
		userInput, err = handleFileReference(userInput)
		if err != nil {
			fmt.Printf("❌ Error with file selection: %v\n", err)
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

		var totalMetrics *ChatMetrics
		maxIterations := 10
		for iteration := 0; iteration < maxIterations; iteration++ {
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

			assistantMsg := sdk.Message{
				Role:    sdk.Assistant,
				Content: assistantMessage,
			}
			if len(assistantToolCalls) > 0 {
				assistantMsg.ToolCalls = &assistantToolCalls
			}
			conversation = append(conversation, assistantMsg)

			if len(assistantToolCalls) == 0 {
				break
			}

			toolExecutionFailed := false
			for _, toolCall := range assistantToolCalls {
				toolResult, err := executeToolCall(cfg, toolCall.Function.Name, toolCall.Function.Arguments)
				if err != nil {
					fmt.Printf("❌ Tool execution failed: %v\n", err)
					toolExecutionFailed = true
					break
				} else {
					fmt.Printf("✅ Tool result:\n%s\n", toolResult)
					conversation = append(conversation, sdk.Message{
						Role:       sdk.Tool,
						Content:    toolResult,
						ToolCallId: &toolCall.Id,
					})
				}
			}

			if toolExecutionFailed {
				conversation = conversation[:len(conversation)-1]
				fmt.Printf("\n❌ Tool execution was cancelled. Please try a different request.\n")
				break
			}

			fmt.Printf("\n%s: ", selectedModel)
		}

		displayChatMetrics(totalMetrics)
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
	case "/compact":
		err := compactConversation(*conversation, *selectedModel)
		if err != nil {
			fmt.Printf("❌ Error creating compact file: %v\n", err)
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
	fmt.Println("  /compact         - Export conversation to markdown file")
	fmt.Println("  /help            - Show this help")
	fmt.Println()
	fmt.Println("File References:")
	fmt.Println("  @                - Interactive file selector dropdown (NEW!)")
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

	result := &streamingResult{
		fullMessage:     &strings.Builder{},
		firstContent:    true,
		activeToolCalls: make(map[int]*sdk.ChatCompletionMessageToolCall),
		spinnerActive:   spinnerActive,
		mu:              mu,
		cfg:             cfg,
	}

	err = processStreamingEvents(events, result)
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

type streamingResult struct {
	fullMessage     *strings.Builder
	firstContent    bool
	usage           *sdk.CompletionUsage
	toolCalls       []sdk.ChatCompletionMessageToolCall
	activeToolCalls map[int]*sdk.ChatCompletionMessageToolCall
	spinnerActive   *bool
	mu              *sync.Mutex
	cfg             *config.Config
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

func processStreamingEvents(events <-chan sdk.SSEvent, result *streamingResult) error {
	for event := range events {
		if event.Event == nil {
			continue
		}

		switch *event.Event {
		case sdk.ContentDelta:
			if err := handleContentDelta(event, result); err != nil {
				return err
			}
		case sdk.StreamEnd:
			handleStreamEnd(result)
			return nil
		case "error":
			return handleStreamError(event, result)
		}
	}
	return nil
}

func handleContentDelta(event sdk.SSEvent, result *streamingResult) error {
	if event.Data == nil {
		return nil
	}

	var streamResponse sdk.CreateChatCompletionStreamResponse
	if err := json.Unmarshal(*event.Data, &streamResponse); err != nil {
		return nil
	}

	for _, choice := range streamResponse.Choices {
		handleContentChoice(choice, result)
		handleToolCallsChoice(choice, result)
	}

	if streamResponse.Usage != nil {
		result.usage = streamResponse.Usage
	}

	return nil
}

func handleContentChoice(choice sdk.ChatCompletionStreamChoice, result *streamingResult) {
	if choice.Delta.Content == "" {
		return
	}

	if result.firstContent {
		stopSpinner(result)
		result.firstContent = false
	}

	fmt.Print(choice.Delta.Content)
	result.fullMessage.WriteString(choice.Delta.Content)
}

func handleToolCallsChoice(choice sdk.ChatCompletionStreamChoice, result *streamingResult) {
	if len(choice.Delta.ToolCalls) == 0 {
		return
	}

	if result.firstContent {
		stopSpinner(result)
		result.firstContent = false
	}

	for _, deltaToolCall := range choice.Delta.ToolCalls {
		handleToolCallDelta(deltaToolCall, result)
	}
}

func handleToolCallDelta(deltaToolCall sdk.ChatCompletionMessageToolCallChunk, result *streamingResult) {
	index := deltaToolCall.Index

	if result.activeToolCalls[index] == nil {
		result.activeToolCalls[index] = &sdk.ChatCompletionMessageToolCall{
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

	if deltaToolCall.Function.Arguments != "" {
		result.activeToolCalls[index].Function.Arguments += deltaToolCall.Function.Arguments
	}
}

func handleStreamEnd(result *streamingResult) {
	stopSpinner(result)
	var toolCalls []sdk.ChatCompletionMessageToolCall
	for _, toolCall := range result.activeToolCalls {
		if toolCall.Function.Name != "" {
			fmt.Printf(" with arguments: %s\n", toolCall.Function.Arguments)
			toolCalls = append(toolCalls, *toolCall)
		}
	}
	result.toolCalls = toolCalls
}

func handleStreamError(event sdk.SSEvent, result *streamingResult) error {
	stopSpinner(result)

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

func stopSpinner(result *streamingResult) {
	result.mu.Lock()
	*result.spinnerActive = false
	result.mu.Unlock()
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

	searcher := func(input string, index int) bool {
		file := files[index]
		name := strings.ReplaceAll(strings.ToLower(file), " ", "")
		input = strings.ReplaceAll(strings.ToLower(input), " ", "")
		return strings.Contains(name, input)
	}

	prompt := promptui.Select{
		Label:    "Select a file to include (type to search, ESC to cancel)",
		Items:    files,
		Size:     15,
		Searcher: searcher,
		Templates: &promptui.SelectTemplates{
			Label:    "{{ . }}?",
			Active:   "▶ {{ . | cyan | bold }}",
			Inactive: "  {{ . }}",
			Selected: "✓ Selected file: {{ . | green | bold }}",
		},
	}

	_, result, err := prompt.Run()
	if err != nil {
		if err == promptui.ErrInterrupt {
			return "", fmt.Errorf("file selection cancelled")
		}
		return "", fmt.Errorf("file selection failed: %w", err)
	}

	return result, nil
}

// handleFileReference processes "@" references in user input
func handleFileReference(input string) (string, error) {
	// Check if input is exactly "@" - trigger interactive selection
	if strings.TrimSpace(input) == "@" {
		selectedFile, err := selectFileInteractively()
		if err != nil {
			return "", err
		}
		return "@" + selectedFile, nil
	}

	// Check if input ends with "@" - trigger interactive selection and append
	if strings.HasSuffix(strings.TrimSpace(input), "@") {
		selectedFile, err := selectFileInteractively()
		if err != nil {
			return "", err
		}
		// Remove the trailing @ and append the selected file reference
		trimmed := strings.TrimSpace(input)
		prefix := trimmed[:len(trimmed)-1]
		return prefix + "@" + selectedFile, nil
	}

	// Return input unchanged if no "@" trigger detected
	return input, nil
}

func compactConversation(conversation []sdk.Message, selectedModel string) error {
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
	content.WriteString(fmt.Sprintf("**Model:** %s\n", selectedModel))
	content.WriteString(fmt.Sprintf("**Date:** %s\n", time.Now().Format("2006-01-02 15:04:05")))
	content.WriteString(fmt.Sprintf("**Total Messages:** %d\n\n", len(conversation)))
	content.WriteString("---\n\n")

	for i, msg := range conversation {
		var role string
		switch msg.Role {
		case sdk.User:
			role = "👤 **You**"
		case sdk.Assistant:
			role = "🤖 **Assistant**"
		case sdk.System:
			role = "⚙️ **System**"
		case sdk.Tool:
			role = "🔧 **Tool Result**"
		default:
			role = fmt.Sprintf("**%s**", string(msg.Role))
		}

		content.WriteString(fmt.Sprintf("## Message %d - %s\n\n", i+1, role))

		if msg.Content != "" {
			content.WriteString(msg.Content)
			content.WriteString("\n\n")
		}

		if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			content.WriteString("### Tool Calls\n\n")
			for _, toolCall := range *msg.ToolCalls {
				content.WriteString(fmt.Sprintf("**Tool:** %s\n\n", toolCall.Function.Name))
				if toolCall.Function.Arguments != "" {
					content.WriteString("**Arguments:**\n```json\n")
					content.WriteString(toolCall.Function.Arguments)
					content.WriteString("\n```\n\n")
				}
			}
		}

		if msg.ToolCallId != nil {
			content.WriteString(fmt.Sprintf("*Tool Call ID: %s*\n\n", *msg.ToolCallId))
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

func init() {
	rootCmd.AddCommand(chatCmd)
}
