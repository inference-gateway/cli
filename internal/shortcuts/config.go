package shortcuts

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/inference-gateway/cli/config"
	"github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// ConfigShortcut allows runtime configuration management
type ConfigShortcut struct {
	config        *config.Config
	reloadFunc    func() (*config.Config, error)
	configService interface {
		SetValue(key, value string) error
	}
}

// NewConfigShortcut creates a new config shortcut
func NewConfigShortcut(cfg *config.Config, reloadFunc func() (*config.Config, error), configService interface {
	SetValue(key, value string) error
}) *ConfigShortcut {
	return &ConfigShortcut{
		config:        cfg,
		reloadFunc:    reloadFunc,
		configService: configService,
	}
}

func (c *ConfigShortcut) GetName() string        { return "config" }
func (c *ConfigShortcut) GetDescription() string { return "Manage configuration settings" }
func (c *ConfigShortcut) GetUsage() string {
	return "/config <show|get|set|reload> [key] [value]"
}

func (c *ConfigShortcut) CanExecute(args []string) bool {
	if len(args) == 0 {
		return false
	}

	subcommand := args[0]
	switch subcommand {
	case "show":
		return len(args) == 1
	case "get":
		return len(args) == 2
	case "set":
		return len(args) == 3
	case "reload":
		return len(args) == 1
	default:
		return false
	}
}

func (c *ConfigShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	subcommand := args[0]

	switch subcommand {
	case "show":
		return c.executeShow()
	case "get":
		return c.executeGet(args[1])
	case "set":
		return c.executeSet(args[1], args[2])
	case "reload":
		return c.executeReload()
	default:
		return ShortcutResult{
			Output:  fmt.Sprintf("Unknown subcommand: %s. Use show, get, set, or reload", subcommand),
			Success: false,
		}, nil
	}
}

func (c *ConfigShortcut) executeShow() (ShortcutResult, error) {
	var output strings.Builder
	output.WriteString("## Current Configuration\n\n")

	// Gateway settings
	output.WriteString("### 🌐 Gateway\n")
	output.WriteString(fmt.Sprintf("• **URL**: `%s`\n", c.config.Gateway.URL))
	output.WriteString(fmt.Sprintf("• **Timeout**: `%d`s\n", c.config.Gateway.Timeout))
	if c.config.Gateway.APIKey != "" {
		output.WriteString("• **API Key**: *[configured]*\n")
	} else {
		output.WriteString("• **API Key**: *[not set]*\n")
	}

	// Agent settings
	output.WriteString("\n### 🤖 Agent\n")
	if c.config.Agent.Model != "" {
		output.WriteString(fmt.Sprintf("• **Model**: `%s`\n", c.config.Agent.Model))
	} else {
		output.WriteString("• **Model**: *[not set]*\n")
	}
	output.WriteString(fmt.Sprintf("• **Verbose Tools**: `%v`\n", c.config.Agent.VerboseTools))
	output.WriteString(fmt.Sprintf("• **Max Turns**: `%d`\n", c.config.Agent.MaxTurns))
	output.WriteString(fmt.Sprintf("• **Max Tokens**: `%d`\n", c.config.Agent.MaxTokens))

	// Tools settings
	output.WriteString("\n### 🔧 Tools\n")
	if c.config.Tools.Enabled {
		output.WriteString(fmt.Sprintf("• **Enabled**: %s\n", icons.StyledCheckMark()))
		output.WriteString("• **Individual Tools**:\n")
		output.WriteString(fmt.Sprintf("  - **Bash**: %s\n", formatBool(c.config.Tools.Bash.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Read**: %s\n", formatBool(c.config.Tools.Read.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Write**: %s\n", formatBool(c.config.Tools.Write.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Edit**: %s\n", formatBool(c.config.Tools.Edit.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Grep**: %s\n", formatBool(c.config.Tools.Grep.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Web Fetch**: %s\n", formatBool(c.config.Tools.WebFetch.Enabled)))
		output.WriteString(fmt.Sprintf("  - **Web Search**: %s\n", formatBool(c.config.Tools.WebSearch.Enabled)))
	} else {
		output.WriteString(fmt.Sprintf("• **Enabled**: %s\n", icons.StyledCrossMark()))
	}

	// Optimization settings
	output.WriteString("\n### ⚡ Optimization\n")
	if c.config.Agent.Optimization.Enabled {
		output.WriteString(fmt.Sprintf("• **Enabled**: %s\n", icons.StyledCheckMark()))
		output.WriteString(fmt.Sprintf("• **Max History**: `%d`\n", c.config.Agent.Optimization.MaxHistory))
		output.WriteString(fmt.Sprintf("• **Compact Threshold**: `%d`\n", c.config.Agent.Optimization.CompactThreshold))
	} else {
		output.WriteString(fmt.Sprintf("• **Enabled**: %s\n", icons.StyledCrossMark()))
	}

	return ShortcutResult{
		Output:  output.String(),
		Success: true,
	}, nil
}

func (c *ConfigShortcut) executeGet(key string) (ShortcutResult, error) {
	value, err := c.getConfigValue(key)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Error getting config value: %v", err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:  fmt.Sprintf("%s: %v", key, value),
		Success: true,
	}, nil
}

func (c *ConfigShortcut) executeSet(key, value string) (ShortcutResult, error) {
	if c.configService == nil {
		return ShortcutResult{
			Output:  "⚠️  Config setting not available - please exit chat mode and use CLI commands like 'infer config agent set-model <model>' to make changes.",
			Success: false,
		}, nil
	}

	if err := c.configService.SetValue(key, value); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Failed to set config value: %v", icons.StyledCrossMark(), err),
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("%s Successfully set **%s** = `%s`", icons.StyledCheckMark(), key, value),
		Success:    true,
		SideEffect: SideEffectReloadConfig,
	}, nil
}

func (c *ConfigShortcut) executeReload() (ShortcutResult, error) {
	if c.reloadFunc == nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Config reload not supported - please exit and restart chat mode to apply config changes", icons.StyledCrossMark()),
			Success: false,
		}, nil
	}

	newConfig, err := c.reloadFunc()
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Failed to reload config: %v", icons.StyledCrossMark(), err),
			Success: false,
		}, nil
	}

	// Update the config reference
	*c.config = *newConfig

	return ShortcutResult{
		Output:     fmt.Sprintf("%s Configuration reloaded successfully from disk", icons.StyledCheckMark()),
		Success:    true,
		SideEffect: SideEffectReloadConfig,
		Data:       newConfig,
	}, nil
}

// getConfigValue retrieves a config value using dot notation (e.g., "agent.model")
func (c *ConfigShortcut) getConfigValue(key string) (interface{}, error) {
	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty key")
	}

	// Use reflection to traverse the config structure
	value := reflect.ValueOf(c.config).Elem()
	for _, part := range parts {
		if !value.IsValid() {
			return nil, fmt.Errorf("invalid config path: %s", key)
		}

		// Handle struct fields
		if value.Kind() == reflect.Struct {
			field := c.findField(value, part)

			if !field.IsValid() {
				return nil, fmt.Errorf("field not found: %s in path: %s", part, key)
			}
			value = field
		} else {
			return nil, fmt.Errorf("cannot traverse non-struct type at: %s in path: %s", part, key)
		}
	}

	if !value.IsValid() {
		return nil, fmt.Errorf("invalid value at path: %s", key)
	}

	return c.formatValue(value), nil
}

// findField finds a field in a struct by checking field name, yaml tag, and mapstructure tag
func (c *ConfigShortcut) findField(structValue reflect.Value, fieldName string) reflect.Value {
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structType.Field(i)
		fieldValue := structValue.Field(i)

		if strings.EqualFold(field.Name, fieldName) {
			return fieldValue
		}

		if yamlTag := field.Tag.Get("yaml"); yamlTag != "" {
			tagName := strings.Split(yamlTag, ",")[0]
			if strings.EqualFold(tagName, fieldName) {
				return fieldValue
			}
		}

		if mapTag := field.Tag.Get("mapstructure"); mapTag != "" {
			tagName := strings.Split(mapTag, ",")[0]
			if strings.EqualFold(tagName, fieldName) {
				return fieldValue
			}
		}
	}

	return reflect.Value{}
}

// formatValue formats a reflect.Value for display
func (c *ConfigShortcut) formatValue(value reflect.Value) interface{} {
	switch value.Kind() {
	case reflect.String:
		return value.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return value.Uint()
	case reflect.Bool:
		return value.Bool()
	case reflect.Ptr:
		if value.IsNil() {
			return nil
		}
		return c.formatValue(value.Elem())
	case reflect.Slice:
		return c.formatSlice(value)
	case reflect.Struct:
		return c.formatStruct(value)
	default:
		return value.Interface()
	}
}

// formatSlice formats a slice for better readability
func (c *ConfigShortcut) formatSlice(value reflect.Value) string {
	var items []string
	for i := 0; i < value.Len(); i++ {
		item := c.formatValue(value.Index(i))
		items = append(items, fmt.Sprintf("%v", item))
	}
	return fmt.Sprintf("[%s]", strings.Join(items, ", "))
}

// formatStruct formats a struct for better readability
func (c *ConfigShortcut) formatStruct(value reflect.Value) string {
	var output strings.Builder
	structType := value.Type()

	output.WriteString("{\n")
	for i := 0; i < value.NumField(); i++ {
		field := structType.Field(i)
		fieldValue := value.Field(i)

		// Skip unexported fields
		if !fieldValue.CanInterface() {
			continue
		}

		// Get field name from yaml tag if available, otherwise use field name
		fieldName := field.Name
		if yamlTag := field.Tag.Get("yaml"); yamlTag != "" {
			tagName := strings.Split(yamlTag, ",")[0]
			if tagName != "" && tagName != "-" {
				fieldName = tagName
			}
		}

		formattedValue := c.formatValue(fieldValue)
		output.WriteString(fmt.Sprintf("  %s: %v\n", fieldName, formattedValue))
	}
	output.WriteString("}")

	return output.String()
}

// formatBool formats a boolean as checkmark/X for better readability
func formatBool(b bool) string {
	if b {
		return icons.CheckMark
	}
	return icons.CrossMark
}
