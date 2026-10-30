package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	skills "github.com/inference-gateway/cli/internal/skills"
)

// SkillTool loads a skill's SKILL.md by name so the model never has to guess
// a path, and so a loaded skill is identifiable in the conversation by name.
type SkillTool struct {
	config    *config.Config
	skills    agentdomain.SkillsService
	formatter BaseFormatter
}

// NewSkillTool creates the Skill tool over the skills port.
func NewSkillTool(cfg *config.Config, skills agentdomain.SkillsService) *SkillTool {
	return &SkillTool{config: cfg, skills: skills, formatter: NewBaseFormatter(ToolSkill)}
}

// Manifest returns the tool's manifest.
func (t *SkillTool) Manifest() agentdomain.ToolManifest {
	return toolManifests.MustGet(ToolSkill)
}

// Definition returns the tool definition for the LLM.
func (t *SkillTool) Definition() sdk.ChatCompletionTool {
	return t.Manifest().Definition()
}

// Execute resolves the skill and returns its SKILL.md. A catalog skill that is
// not installed yet is downloaded only outside chat, where nobody can approve.
func (t *SkillTool) Execute(ctx context.Context, args map[string]any) (*agentdomain.ToolExecutionResult, error) {
	start := time.Now()
	name := skillBareName(args["name"])
	fail := func(msg string) *agentdomain.ToolExecutionResult {
		return &agentdomain.ToolExecutionResult{ToolName: ToolSkill, Arguments: args, Duration: time.Since(start), Error: msg}
	}

	sk, ok := t.skills.Get(name)
	if !ok {
		return fail(fmt.Sprintf("unknown skill %q - pick a name from AVAILABLE SKILLS", name)), nil
	}
	if sk.Path == "" {
		if agentdomain.GetChatHandler(ctx) != nil {
			return fail(fmt.Sprintf("skill %q is in the catalog but not installed - ask the user to run /%s to install it", name, name)), nil
		}
		if sk, ok = t.skills.Discover(ctx, name); !ok || sk.Path == "" {
			return fail(fmt.Sprintf("skill %q could not be installed from the catalog", name)), nil
		}
	}

	body, err := LoadSkillBody(sk.Path)
	if err != nil {
		return fail(fmt.Sprintf("cannot read %s: %v", sk.Path, err)), nil
	}

	return &agentdomain.ToolExecutionResult{
		ToolName:  ToolSkill,
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      &agentdomain.SkillToolResult{Name: sk.DisplayName(), Path: sk.Path, Content: body},
	}, nil
}

// LoadSkillBody reads a SKILL.md and returns its body without the YAML
// frontmatter: the name and description already reach the model through the
// skills listing, and the raw frontmatter only clutters the conversation.
func LoadSkillBody(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	_, body, err := skills.SplitFrontmatter(raw)
	if err != nil {
		return strings.TrimSpace(string(raw)), nil
	}
	return strings.TrimSpace(body), nil
}

func skillBareName(arg any) string {
	name, _ := arg.(string)
	if _, bare, ok := strings.Cut(name, ":"); ok {
		return strings.TrimSpace(bare)
	}
	return strings.TrimSpace(name)
}

// Validate checks the arguments.
func (t *SkillTool) Validate(args map[string]any) error {
	if skillBareName(args["name"]) == "" {
		return fmt.Errorf("name parameter is required and must be a non-empty string")
	}
	return nil
}

// IsEnabled reports whether skills are on and a skills service is wired.
func (t *SkillTool) IsEnabled() bool {
	return t.config.Tools.Enabled && t.config.GetAgentConfig().Skills.Enabled && t.skills != nil
}

// FormatResult formats the result for the given consumer.
func (t *SkillTool) FormatResult(result *agentdomain.ToolExecutionResult, formatType agentdomain.FormatterType) string {
	switch formatType {
	case agentdomain.FormatterLLM:
		return t.FormatForLLM(result)
	case agentdomain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.formatter.FormatAsJSON(result)
	}
}

// FormatPreview is the one-line summary shown in the UI.
func (t *SkillTool) FormatPreview(result *agentdomain.ToolExecutionResult) string {
	if data, ok := result.Data.(*agentdomain.SkillToolResult); ok && data != nil {
		return fmt.Sprintf("Loaded skill %s", data.Name)
	}
	if result.Error != "" {
		return "Skill failed: " + result.Error
	}
	return "Skill"
}

// FormatForLLM returns the result tree with the skill body under Result, the
// same shape Read uses, which the TUI's expanded card relies on for theming.
func (t *SkillTool) FormatForLLM(result *agentdomain.ToolExecutionResult) string {
	data, ok := result.Data.(*agentdomain.SkillToolResult)
	if !ok || data == nil {
		return t.formatter.FormatExpanded(result, "")
	}
	return t.formatter.FormatExpanded(result, FormatSkillForLLM(data.Name, data.Path, data.Content))
}

// FormatSkillForLLM is the Skill tool's result text. The compaction restorer
// re-emits restored skills in this exact shape.
func FormatSkillForLLM(name, path, content string) string {
	return fmt.Sprintf("Skill: %s\nPath: %s\n\n%s", name, path, content)
}
