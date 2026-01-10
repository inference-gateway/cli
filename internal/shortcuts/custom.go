package shortcuts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	domain "github.com/inference-gateway/cli/internal/domain"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
)

// SnippetConfig represents the snippet generation configuration
type SnippetConfig struct {
	Prompt   string `yaml:"prompt"`
	Template string `yaml:"template"`
}

// SubcommandConfig represents a subcommand for shortcuts with autocomplete
type SubcommandConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Command     string         `yaml:"command,omitempty"`
	Args        []string       `yaml:"args,omitempty"`
	WorkingDir  string         `yaml:"working_dir,omitempty"`
	Snippet     *SnippetConfig `yaml:"snippet,omitempty"`
}

// CustomShortcutConfig represents a user-defined shortcut configuration
type CustomShortcutConfig struct {
	Name          string             `yaml:"name"`
	Description   string             `yaml:"description"`
	Command       string             `yaml:"command,omitempty"`
	Args          []string           `yaml:"args,omitempty"`
	WorkingDir    string             `yaml:"working_dir,omitempty"`
	Tool          string             `yaml:"tool,omitempty"`
	ToolArgs      map[string]any     `yaml:"tool_args,omitempty"`
	Snippet       *SnippetConfig     `yaml:"snippet,omitempty"`
	PassSessionID bool               `yaml:"pass_session_id,omitempty"`
	Subcommands   []SubcommandConfig `yaml:"subcommands,omitempty"`
}

// CustomShortcutsConfig represents the structure of a custom shortcuts YAML file
type CustomShortcutsConfig struct {
	Shortcuts []CustomShortcutConfig `yaml:"shortcuts"`
}

// CustomShortcut implements the Shortcut interface for user-defined shortcuts
type CustomShortcut struct {
	config       CustomShortcutConfig
	client       sdk.Client
	modelService domain.ModelService
	imageService domain.ImageService
	toolService  domain.ToolService
}

// NewCustomShortcut creates a new custom shortcut from configuration
func NewCustomShortcut(config CustomShortcutConfig, client sdk.Client, modelService domain.ModelService, imageService domain.ImageService, toolService domain.ToolService) *CustomShortcut {
	return &CustomShortcut{
		config:       config,
		client:       client,
		modelService: modelService,
		imageService: imageService,
		toolService:  toolService,
	}
}

func (c *CustomShortcut) GetName() string {
	return c.config.Name
}

func (c *CustomShortcut) GetDescription() string {
	return c.config.Description
}

func (c *CustomShortcut) GetUsage() string {
	return fmt.Sprintf("/%s", c.config.Name)
}

func (c *CustomShortcut) CanExecute(args []string) bool {
	return true
}

// Subcommand represents a subcommand for autocomplete
type Subcommand struct {
	Name        string
	Description string
}

// GetSubcommands returns the list of subcommands for this shortcut
func (c *CustomShortcut) GetSubcommands() []Subcommand {
	result := make([]Subcommand, len(c.config.Subcommands))
	for i, sc := range c.config.Subcommands {
		result[i] = Subcommand{
			Name:        sc.Name,
			Description: sc.Description,
		}
	}
	return result
}

// extractImageURLs extracts all image URLs from both HTML <img> tags and markdown ![](url) syntax
func extractImageURLs(text string) []string {
	var urls []string
	urlSet := make(map[string]bool)

	htmlImgRegex := regexp.MustCompile(`<img[^>]*src="([^"]+)"[^>]*\/?>`)
	htmlMatches := htmlImgRegex.FindAllStringSubmatch(text, -1)
	for _, match := range htmlMatches {
		if len(match) > 1 && !urlSet[match[1]] {
			urlSet[match[1]] = true
			urls = append(urls, match[1])
		}
	}

	mdImgRegex := regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	mdMatches := mdImgRegex.FindAllStringSubmatch(text, -1)
	for _, match := range mdMatches {
		if len(match) > 2 && !urlSet[match[2]] {
			urlSet[match[2]] = true
			urls = append(urls, match[2])
		}
	}

	return urls
}

// extractAllImageURLs extracts image URLs from both top-level text and nested JSON structures
// This handles GitHub issues with images in comments
func extractAllImageURLs(text string) []string {
	urlSet := make(map[string]bool)
	var urls []string

	topLevelURLs := extractImageURLs(text)
	for _, url := range topLevelURLs {
		if !urlSet[url] {
			urlSet[url] = true
			urls = append(urls, url)
		}
	}

	var jsonData map[string]any
	if err := json.Unmarshal([]byte(text), &jsonData); err == nil {
		extractImagesFromJSON(jsonData, urlSet, &urls)
	}

	return urls
}

// extractImagesFromJSON recursively searches JSON for image URLs
func extractImagesFromJSON(data any, urlSet map[string]bool, urls *[]string) {
	switch v := data.(type) {
	case map[string]any:
		for _, value := range v {
			extractImagesFromJSON(value, urlSet, urls)
		}
	case []any:
		for _, item := range v {
			extractImagesFromJSON(item, urlSet, urls)
		}
	case string:
		imgURLs := extractImageURLs(v)
		for _, url := range imgURLs {
			if !urlSet[url] {
				urlSet[url] = true
				*urls = append(*urls, url)
			}
		}
	}
}

// fillTemplate replaces placeholders in template with values from data map
// Supports {field} for data fields and {llm} for LLM-generated content
func fillTemplate(template string, data map[string]string) string {
	result := template

	placeholderRegex := regexp.MustCompile(`\{([^}]+)\}`)
	result = placeholderRegex.ReplaceAllStringFunc(result, func(match string) string {
		key := match[1 : len(match)-1]

		if value, exists := data[key]; exists {
			return value
		}

		return match
	})

	return result
}

func (c *CustomShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if c.config.Tool != "" {
		return c.executeWithTool(ctx, args)
	}

	subcommand, remainingArgs := c.findSubcommand(args)
	cmdConfig := c.resolveCommandConfig(subcommand)
	cmdArgs := c.buildCommandArgs(cmdConfig, remainingArgs, ctx)

	cmd := exec.CommandContext(ctx, cmdConfig.command, cmdArgs...)
	if cmdConfig.workingDir != "" {
		cmd.Dir = cmdConfig.workingDir
	}

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Command failed: %s\n\nOutput:\n%s", icons.StyledCrossMark(), err.Error(), outputStr),
			Success: false,
		}, nil
	}

	if cmdConfig.snippet != nil {
		return c.executeWithSnippet(ctx, outputStr, cmdConfig.snippet)
	}

	return c.formatOutput(outputStr)
}

// commandConfig holds resolved command configuration
type commandConfig struct {
	command    string
	args       []string
	workingDir string
	snippet    *SnippetConfig
}

// findSubcommand finds a matching subcommand and returns it with remaining args
func (c *CustomShortcut) findSubcommand(args []string) (*SubcommandConfig, []string) {
	if len(args) == 0 || len(c.config.Subcommands) == 0 {
		return nil, args
	}

	for i := range c.config.Subcommands {
		if c.config.Subcommands[i].Name == args[0] {
			return &c.config.Subcommands[i], args[1:]
		}
	}

	return nil, args
}

// resolveCommandConfig determines which command configuration to use
func (c *CustomShortcut) resolveCommandConfig(subcommand *SubcommandConfig) commandConfig {
	cfg := commandConfig{
		command:    c.config.Command,
		args:       append([]string{}, c.config.Args...),
		workingDir: c.config.WorkingDir,
		snippet:    c.config.Snippet,
	}

	if subcommand == nil {
		return cfg
	}

	if subcommand.Command != "" {
		cfg.command = subcommand.Command
		cfg.args = append([]string{}, subcommand.Args...)
	} else {
		cfg.args = append(cfg.args, subcommand.Name)
		cfg.args = append(cfg.args, subcommand.Args...)
	}

	if subcommand.WorkingDir != "" {
		cfg.workingDir = subcommand.WorkingDir
	}
	if subcommand.Snippet != nil {
		cfg.snippet = subcommand.Snippet
	}

	return cfg
}

// buildCommandArgs builds the final command arguments
func (c *CustomShortcut) buildCommandArgs(cfg commandConfig, args []string, ctx context.Context) []string {
	if c.config.PassSessionID {
		if sessionID := ctx.Value(domain.SessionIDKey); sessionID != nil {
			if sessionIDStr, ok := sessionID.(string); ok {
				args = append(args, sessionIDStr)
			}
		}
	}

	cmdArgs := cfg.args
	if (cfg.command == "bash" || cfg.command == "sh") && len(cmdArgs) >= 2 && cmdArgs[0] == "-c" {
		cmdArgs = append(cmdArgs, cfg.command)
		cmdArgs = append(cmdArgs, args...)
	} else {
		cmdArgs = append(cmdArgs, args...)
	}

	return cmdArgs
}

// formatOutput formats the command output with appropriate styling
func (c *CustomShortcut) formatOutput(outputStr string) (ShortcutResult, error) {
	imageURLs := extractAllImageURLs(outputStr)
	var imageAttachments []domain.ImageAttachment

	if len(imageURLs) > 0 && c.imageService != nil {
		for _, url := range imageURLs {
			if attachment, err := c.imageService.ReadImageFromURL(url); err == nil {
				imageAttachments = append(imageAttachments, *attachment)
			}
		}
	}

	isMarkdown := strings.Contains(outputStr, "| ") && strings.Contains(outputStr, " |") && strings.Contains(outputStr, "---")

	if isMarkdown {
		if len(imageAttachments) > 0 {
			return ShortcutResult{
				Output:     outputStr,
				Success:    true,
				SideEffect: SideEffectEmbedImages,
				Data:       imageAttachments,
			}, nil
		}
		return ShortcutResult{
			Output:  outputStr,
			Success: true,
		}, nil
	}

	formattedOutput := fmt.Sprintf("```json\n%s\n```", outputStr)
	if len(imageAttachments) > 0 {
		return ShortcutResult{
			Output:     formattedOutput,
			Success:    true,
			SideEffect: SideEffectEmbedImages,
			Data:       imageAttachments,
		}, nil
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s **%s completed**\n\n```\n%s\n```", icons.StyledCheckMark(), c.config.Name, outputStr),
		Success: true,
	}, nil
}

// executeWithTool executes a tool directly without using AI
func (c *CustomShortcut) executeWithTool(ctx context.Context, _ []string) (ShortcutResult, error) {
	if c.toolService == nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool service not available", icons.StyledCrossMark()),
			Success: false,
		}, nil
	}

	if !c.toolService.IsToolEnabled(c.config.Tool) {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool '%s' is not enabled", icons.StyledCrossMark(), c.config.Tool),
			Success: false,
		}, nil
	}

	toolArgs := make(map[string]any)
	if c.config.ToolArgs != nil {
		toolArgs = c.config.ToolArgs
	}

	if err := c.toolService.ValidateTool(c.config.Tool, toolArgs); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool validation failed: %v", icons.StyledCrossMark(), err),
			Success: false,
		}, nil
	}

	argsJSON, err := json.Marshal(toolArgs)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Failed to marshal tool arguments: %v", icons.StyledCrossMark(), err),
			Success: false,
		}, nil
	}

	toolCall := sdk.ChatCompletionMessageToolCallFunction{
		Name:      c.config.Tool,
		Arguments: string(argsJSON),
	}

	ctx = context.WithValue(ctx, domain.DirectExecutionKey, true)
	result, err := c.toolService.ExecuteToolDirect(ctx, toolCall)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool execution failed: %v", icons.StyledCrossMark(), err),
			Success: false,
		}, nil
	}

	if !result.Success {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool '%s' failed", icons.StyledCrossMark(), c.config.Tool),
			Success: false,
		}, nil
	}

	tool, err := c.toolService.GetTool(c.config.Tool)
	if err != nil {
		if data, ok := result.Data.(map[string]any); ok {
			dataJSON, _ := json.MarshalIndent(data, "", "  ")
			return ShortcutResult{
				Output:  fmt.Sprintf("%s **%s completed**\n\n```json\n%s\n```", icons.StyledCheckMark(), result.ToolName, string(dataJSON)),
				Success: true,
			}, nil
		}
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Tool '%s' executed successfully", icons.StyledCheckMark(), result.ToolName),
			Success: true,
		}, nil
	}

	output := tool.FormatResult(result, domain.FormatterUI)
	return ShortcutResult{
		Output:  output,
		Success: true,
	}, nil
}

// executeWithSnippet handles snippet generation with LLM (async)
func (c *CustomShortcut) executeWithSnippet(ctx context.Context, commandOutput string, snippet *SnippetConfig) (ShortcutResult, error) {
	var jsonData map[string]any
	if err := json.Unmarshal([]byte(commandOutput), &jsonData); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Failed to parse command output as JSON: %v\n\nOutput:\n%s", icons.StyledCrossMark(), err, commandOutput),
			Success: false,
		}, nil
	}

	dataMap := make(map[string]string)
	for key, value := range jsonData {
		dataMap[key] = fmt.Sprintf("%v", value)
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("%s Generating snippet with AI...", icons.StyledCheckMark()),
		Success:    true,
		SideEffect: SideEffectGenerateSnippet,
		Data: map[string]any{
			"context":        ctx,
			"dataMap":        dataMap,
			"customShortcut": c,
			"shortcutName":   c.config.Name,
			"snippet":        snippet,
		},
	}, nil
}

// GenerateSnippet generates the final snippet by calling LLM (called async from handler)
func (c *CustomShortcut) GenerateSnippet(ctx context.Context, dataMap map[string]string, snippet *SnippetConfig) (string, error) {
	filledPrompt := fillTemplate(snippet.Prompt, dataMap)

	llmResponse, err := c.callLLM(ctx, filledPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate snippet with LLM: %w", err)
	}

	dataMap["llm"] = llmResponse

	finalSnippet := fillTemplate(snippet.Template, dataMap)

	return finalSnippet, nil
}

// callLLM sends a prompt to the LLM and returns the response
func (c *CustomShortcut) callLLM(ctx context.Context, prompt string) (string, error) {
	if c.client == nil {
		return "", fmt.Errorf("SDK client not available")
	}

	var model string
	if c.modelService != nil {
		model = c.modelService.GetCurrentModel()
	}
	if model == "" {
		return "", fmt.Errorf("no model configured (use /switch to select a model)")
	}

	slashIndex := strings.Index(model, "/")
	if slashIndex == -1 {
		return "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	provider := model[:slashIndex]
	modelName := strings.TrimPrefix(model, provider+"/")
	providerType := sdk.Provider(provider)

	messages := []sdk.Message{
		{
			Role:    sdk.User,
			Content: sdk.NewMessageContent(prompt),
		},
	}

	maxTokens := 1000
	response, err := c.client.
		WithOptions(&sdk.CreateChatCompletionRequest{
			MaxTokens: &maxTokens,
		}).
		WithMiddlewareOptions(&sdk.MiddlewareOptions{
			SkipMCP: true,
		}).
		GenerateContent(ctx, providerType, modelName, messages)

	if err != nil {
		return "", fmt.Errorf("LLM API call failed: %w", err)
	}

	if len(response.Choices) == 0 {
		return "", fmt.Errorf("no response from LLM")
	}

	contentStr, err := response.Choices[0].Message.Content.AsMessageContent0()
	if err != nil {
		return "", fmt.Errorf("failed to extract LLM response content: %w", err)
	}

	return strings.TrimSpace(contentStr), nil
}

// LoadCustomShortcuts loads user-defined shortcuts from shortcuts/ directory within the specified base directory
func LoadCustomShortcuts(baseDir string, client sdk.Client, modelService domain.ModelService, imageService domain.ImageService, toolService domain.ToolService) ([]Shortcut, error) {
	shortcuts := make([]Shortcut, 0)

	shortcutsDir := filepath.Join(baseDir, "shortcuts")

	if _, err := os.Stat(shortcutsDir); os.IsNotExist(err) {
		return shortcuts, nil
	}

	files, err := filepath.Glob(filepath.Join(shortcutsDir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob custom shortcut files: %w", err)
	}

	for _, file := range files {
		shortcutsFromFile, err := loadShortcutsFromFile(file, client, modelService, imageService, toolService)
		if err != nil {
			fmt.Printf("Warning: failed to load shortcuts from %s: %v\n", file, err)
			continue
		}
		shortcuts = append(shortcuts, shortcutsFromFile...)
	}

	return shortcuts, nil
}

// loadShortcutsFromFile loads shortcuts from a specific YAML file
func loadShortcutsFromFile(filename string, client sdk.Client, modelService domain.ModelService, imageService domain.ImageService, toolService domain.ToolService) ([]Shortcut, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filename, err)
	}

	var config CustomShortcutsConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML from %s: %w", filename, err)
	}

	shortcuts := make([]Shortcut, 0, len(config.Shortcuts))
	for _, shortcutConfig := range config.Shortcuts {
		if shortcutConfig.Name == "" {
			fmt.Printf("Warning: shortcut without name found in %s, skipping\n", filename)
			continue
		}
		// Must have either a command or a tool
		if shortcutConfig.Command == "" && shortcutConfig.Tool == "" {
			fmt.Printf("Warning: shortcut '%s' must have either 'command' or 'tool' specified in %s, skipping\n", shortcutConfig.Name, filename)
			continue
		}

		shortcuts = append(shortcuts, NewCustomShortcut(shortcutConfig, client, modelService, imageService, toolService))
	}

	return shortcuts, nil
}
