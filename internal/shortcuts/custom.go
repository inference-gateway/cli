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

// CustomShortcutConfig represents a user-defined shortcut configuration
type CustomShortcutConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Command     string         `yaml:"command"`
	Args        []string       `yaml:"args,omitempty"`
	WorkingDir  string         `yaml:"working_dir,omitempty"`
	Snippet     *SnippetConfig `yaml:"snippet,omitempty"`
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
}

// NewCustomShortcut creates a new custom shortcut from configuration
func NewCustomShortcut(config CustomShortcutConfig, client sdk.Client, modelService domain.ModelService) *CustomShortcut {
	return &CustomShortcut{
		config:       config,
		client:       client,
		modelService: modelService,
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
	command := c.config.Command
	cmdArgs := append(c.config.Args, args...)

	cmd := exec.CommandContext(ctx, command, cmdArgs...)

	if c.config.WorkingDir != "" {
		cmd.Dir = c.config.WorkingDir
	}

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Command failed: %s\n\nOutput:\n%s", icons.StyledCrossMark(), err.Error(), outputStr),
			Success: false,
		}, nil
	}

	if c.config.Snippet != nil {
		return c.executeWithSnippet(ctx, outputStr)
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s **%s completed**\n\n```\n%s\n```", icons.StyledCheckMark(), c.config.Name, outputStr),
		Success: true,
	}, nil
}

// executeWithSnippet handles snippet generation with LLM (async)
func (c *CustomShortcut) executeWithSnippet(ctx context.Context, commandOutput string) (ShortcutResult, error) {
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
		},
	}, nil
}

// GenerateSnippet generates the final snippet by calling LLM (called async from handler)
func (c *CustomShortcut) GenerateSnippet(ctx context.Context, dataMap map[string]string) (string, error) {
	filledPrompt := fillTemplate(c.config.Snippet.Prompt, dataMap)

	llmResponse, err := c.callLLM(ctx, filledPrompt)
	if err != nil {
		return "", fmt.Errorf("failed to generate snippet with LLM: %w", err)
	}

	// Add LLM response to data map
	dataMap["llm"] = llmResponse

	// Fill the final template with all data including LLM response
	finalSnippet := fillTemplate(c.config.Snippet.Template, dataMap)

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
func LoadCustomShortcuts(baseDir string, client sdk.Client, modelService domain.ModelService) ([]Shortcut, error) {
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
		shortcutsFromFile, err := loadShortcutsFromFile(file, client, modelService)
		if err != nil {
			fmt.Printf("Warning: failed to load shortcuts from %s: %v\n", file, err)
			continue
		}
		shortcuts = append(shortcuts, shortcutsFromFile...)
	}

	return shortcuts, nil
}

// loadShortcutsFromFile loads shortcuts from a specific YAML file
func loadShortcutsFromFile(filename string, client sdk.Client, modelService domain.ModelService) ([]Shortcut, error) {
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
		if shortcutConfig.Command == "" {
			fmt.Printf("Warning: shortcut '%s' without command found in %s, skipping\n", shortcutConfig.Name, filename)
			continue
		}

		shortcuts = append(shortcuts, NewCustomShortcut(shortcutConfig, client, modelService))
	}

	return shortcuts, nil
}
