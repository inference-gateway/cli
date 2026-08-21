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
	prompt := c.config.Prompts.Init.Prompt
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
	return `Generate an AGENTS.md at the project root following the open standard at https://agents.md.

AGENTS.md is a README for coding agents - a predictable place for the context and instructions a new contributor would need. It complements (not duplicates) README.md.

Guidelines:
- Keep it concise - aim for ~400 words. Prefer signal over completeness.
- Use standard Markdown with whatever headings fit the project; there is no required structure.
- Cover what actually matters for an agent to be productive: build/test/lint commands, code style, testing, security gotchas, and any non-obvious conventions. Skip anything obvious from the file tree.
- Be specific: real commands, real file paths, real constraints. No filler.

Briefly inspect the project (build system, config files, existing docs) to ground the content, then write the file.`
}
