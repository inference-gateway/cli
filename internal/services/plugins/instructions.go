// Package plugins implements installation and runtime surfacing of Claude
// Code-format plugins: SKILL.md skills plus an optional AGENTS.md instruction
// ruleset. Plugin code (hooks/, commands/) is never executed.
package plugins

import (
	"fmt"
	"os"
	"strings"

	config "github.com/inference-gateway/cli/config"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// Instruction is one enabled plugin's AGENTS.md ruleset, capped for prompt injection.
type Instruction struct {
	PluginName string
	Content    string
	Truncated  bool
	Marker     string
}

// CapInstructions bounds instruction-file content at maxLines lines, then at
// maxChars characters. Returns the capped content and a truncation marker
// ("" when nothing was cut).
func CapInstructions(content string, maxLines, maxChars int) (string, string) {
	marker := ""
	if maxLines > 0 {
		if lines := strings.SplitAfterN(content, "\n", maxLines+1); len(lines) > maxLines {
			content = strings.TrimRight(strings.Join(lines[:maxLines], ""), "\n")
			marker = fmt.Sprintf("[truncated at %d lines]", maxLines)
		}
	}
	if maxChars > 0 && len(content) > maxChars {
		content = content[:maxChars]
		marker = fmt.Sprintf("[truncated at %d chars]", maxChars)
	}
	return content, marker
}

// Instructions reads each enabled plugin's AGENTS.md in registry order,
// skipping plugins without one. Content is read verbatim (never through
// os.ExpandEnv) so plugin-controlled text cannot expand environment variables.
func Instructions(cfg *config.Config) []Instruction {
	if cfg == nil || !cfg.Plugins.Enabled {
		return nil
	}

	maxChars := cfg.Plugins.MaxInstructionsChars
	if maxChars <= 0 {
		maxChars = config.DefaultInstructionsMaxChars
	}
	maxLines := cfg.Plugins.MaxInstructionsLines
	if maxLines <= 0 {
		maxLines = config.DefaultInstructionsMaxLines
	}

	var out []Instruction
	for _, p := range cfg.Plugins.EnabledEntries() {
		path, err := cfg.Plugins.PluginInstructionsPath(p.Name)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Debug("failed to read plugin instructions", "plugin", p.Name, "path", path, "error", err)
			}
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		content, marker := CapInstructions(content, maxLines, maxChars)
		out = append(out, Instruction{PluginName: p.Name, Content: content, Truncated: marker != "", Marker: marker})
	}
	return out
}

// ClaudeCodeAppend composes the --append-system-prompt payload for Claude
// Code mode. The project AGENTS.md is excluded - the claude CLI reads it
// natively, so appending it would double-inject.
func ClaudeCodeAppend(cfg *config.Config) string {
	parts := make([]string, 0, 2)
	if cfg != nil && cfg.Prompts.Agent.SystemPromptClaudeCode != "" {
		parts = append(parts, cfg.Prompts.Agent.SystemPromptClaudeCode)
	}
	if block := InstructionsBlock(cfg); block != "" {
		parts = append(parts, block)
	}
	return strings.Join(parts, "\n\n")
}

// InstructionsBlock renders the enabled plugins' rulesets as labeled system
// prompt sections. Empty when no enabled plugin ships instructions.
func InstructionsBlock(cfg *config.Config) string {
	instructions := Instructions(cfg)
	if len(instructions) == 0 {
		return ""
	}

	var b strings.Builder
	for i, in := range instructions {
		if i > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "PLUGIN INSTRUCTIONS (%s):\n%s", in.PluginName, in.Content)
		if in.Marker != "" {
			b.WriteString("\n" + in.Marker)
		}
	}
	return b.String()
}
