package plugins

import (
	"fmt"
	"os"
	"strings"

	config "github.com/inference-gateway/cli/config"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
)

// Instruction is one enabled plugin's AGENTS.md ruleset, capped for prompt injection.
type Instruction struct {
	PluginName string
	Content    string
	Truncated  bool
	Marker     string
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
		content, marker := formatting.CapInstructions(content, maxLines, maxChars)
		out = append(out, Instruction{PluginName: p.Name, Content: content, Truncated: marker != "", Marker: marker})
	}
	return out
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
			b.WriteByte('\n')
			b.WriteString(in.Marker)
		}
	}
	return b.String()
}
