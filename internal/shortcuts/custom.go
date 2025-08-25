package shortcuts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	colors "github.com/inference-gateway/cli/internal/ui/styles/colors"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
)

// CustomShortcutConfig represents a user-defined shortcut configuration
type CustomShortcutConfig struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Command     string   `yaml:"command"`
	Args        []string `yaml:"args,omitempty"`
	WorkingDir  string   `yaml:"working_dir,omitempty"`
}

// CustomShortcutsConfig represents the structure of a custom shortcuts YAML file
type CustomShortcutsConfig struct {
	Shortcuts []CustomShortcutConfig `yaml:"shortcuts"`
}

// CustomShortcut implements the Shortcut interface for user-defined shortcuts
type CustomShortcut struct {
	config CustomShortcutConfig
}

// NewCustomShortcut creates a new custom shortcut from configuration
func NewCustomShortcut(config CustomShortcutConfig) *CustomShortcut {
	return &CustomShortcut{config: config}
}

func (c *CustomShortcut) GetName() string {
	return c.config.Name
}

func (c *CustomShortcut) GetDescription() string {
	return c.config.Description
}

func (c *CustomShortcut) GetUsage() string {
	usage := fmt.Sprintf("/%s", c.config.Name)
	if len(c.config.Args) > 0 {
		usage += " " + strings.Join(c.config.Args, " ")
	}
	return usage
}

func (c *CustomShortcut) CanExecute(args []string) bool {
	return true // Custom shortcuts can accept any arguments
}

func (c *CustomShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	// Prepare the command
	command := c.config.Command
	cmdArgs := append(c.config.Args, args...)

	// Create the command
	cmd := exec.CommandContext(ctx, command, cmdArgs...)

	// Set working directory if specified
	if c.config.WorkingDir != "" {
		cmd.Dir = c.config.WorkingDir
	}

	// Execute the command
	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("%s Command failed: %s\n\nOutput:\n%s", icons.CrossMark, err.Error(), outputStr),
			Success: false,
		}, nil
	}

	// Format successful output
	return ShortcutResult{
		Output:  fmt.Sprintf("%s %s**%s completed**%s\n\n```\n%s\n```", icons.CheckMark, colors.Green, c.config.Name, colors.Reset, outputStr),
		Success: true,
	}, nil
}

// LoadCustomShortcuts loads user-defined shortcuts from .infer/shortcuts/ directory
func LoadCustomShortcuts(baseDir string) ([]Shortcut, error) {
	shortcuts := make([]Shortcut, 0)

	shortcutsDir := filepath.Join(baseDir, ".infer", "shortcuts")

	// Check if shortcuts directory exists
	if _, err := os.Stat(shortcutsDir); os.IsNotExist(err) {
		// Directory doesn't exist, return empty list (not an error)
		return shortcuts, nil
	}

	// Read all custom-*.yaml files
	files, err := filepath.Glob(filepath.Join(shortcutsDir, "custom-*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob custom shortcut files: %w", err)
	}

	for _, file := range files {
		shortcutsFromFile, err := loadShortcutsFromFile(file)
		if err != nil {
			// Log error but continue with other files
			fmt.Printf("Warning: failed to load shortcuts from %s: %v\n", file, err)
			continue
		}
		shortcuts = append(shortcuts, shortcutsFromFile...)
	}

	return shortcuts, nil
}

// loadShortcutsFromFile loads shortcuts from a specific YAML file
func loadShortcutsFromFile(filename string) ([]Shortcut, error) {
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
		// Validate required fields
		if shortcutConfig.Name == "" {
			fmt.Printf("Warning: shortcut without name found in %s, skipping\n", filename)
			continue
		}
		if shortcutConfig.Command == "" {
			fmt.Printf("Warning: shortcut '%s' without command found in %s, skipping\n", shortcutConfig.Name, filename)
			continue
		}

		shortcuts = append(shortcuts, NewCustomShortcut(shortcutConfig))
	}

	return shortcuts, nil
}
