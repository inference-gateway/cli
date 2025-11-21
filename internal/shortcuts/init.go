package shortcuts

import (
	"context"

	config "github.com/inference-gateway/cli/config"
)

// InitShortcut sets the input field with a configurable prompt for project initialization
type InitShortcut struct {
	config *config.Config
}

// NewInitShortcut creates a new init shortcut
func NewInitShortcut(cfg *config.Config) *InitShortcut {
	return &InitShortcut{
		config: cfg,
	}
}

func (c *InitShortcut) GetName() string { return "init" }
func (c *InitShortcut) GetDescription() string {
	return "Initialize AGENTS.md by setting input with project analysis prompt"
}
func (c *InitShortcut) GetUsage() string { return "/init" }
func (c *InitShortcut) CanExecute(args []string) bool {
	return len(args) == 0
}

func (c *InitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	prompt := c.config.Init.Prompt
	if prompt == "" {
		prompt = defaultInitPrompt()
	}

	return ShortcutResult{
		Output:     "",
		Success:    true,
		SideEffect: SideEffectSetInput,
		Data:       prompt,
	}, nil
}

func defaultInitPrompt() string {
	return `Please analyze this project and generate a comprehensive AGENTS.md file. Use your available tools to examine the project structure, configuration files, documentation, build systems, and development workflow. Focus on creating actionable documentation that will help other AI agents understand how to work effectively with this project.

The AGENTS.md file should include:
- Project overview and main technologies
- Architecture and structure
- Development environment setup
- Key commands (build, test, lint, run)
- Testing instructions
- Project conventions and coding standards
- Important files and configurations

Write the AGENTS.md file to the project root when you have gathered enough information.`
}
