package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	skills "github.com/inference-gateway/cli/internal/skills"
)

// agentsSubdir is the directory (under .infer/ and ~/.infer/) holding Markdown
// subagent definitions, one <name>.md per agent.
const agentsSubdir = "agents"

// markdownAgentNameMax mirrors the 64-char cap used by Claude Code and skills.
const markdownAgentNameMaxLen = 64

// markdownAgentNameRegex enforces the lowercase letters/digits/hyphen/underscore
// charset accepted for named agents.
var markdownAgentNameRegex = regexp.MustCompile(`^[a-z0-9_-]+$`)

// markdownAgent is a reusable subagent preset defined as a Markdown file with
// YAML frontmatter (Claude Code / Gemini CLI compatible): the body is the
// system prompt, the frontmatter the configuration.
type markdownAgent struct {
	name         string
	description  string
	model        string
	tools        []string
	systemPrompt string
	path         string
	// source classifies where the file was loaded from ("project" or "user"),
	// for user-facing listings.
	source string
}

// markdownAgentFrontmatter is deliberately permissive: only the keys we honor
// are declared, everything else (color, temperature, max_turns, mcpServers,
// permissionMode, ...) is accepted and ignored for vendor-file portability.
type markdownAgentFrontmatter struct {
	Name            string `yaml:"name"`
	Description     string `yaml:"description"`
	Model           string `yaml:"model"`
	Tools           any    `yaml:"tools"`
	DisallowedTools any    `yaml:"disallowedTools"`
}

// markdownAgentDirs returns the search paths in precedence order: the project's
// .infer/agents first, then the user-global ~/.infer/agents, so a project can
// override a personal agent of the same name.
func markdownAgentDirs() []string {
	dirs := []string{filepath.Join(config.ConfigDirName, agentsSubdir)}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, config.ConfigDirName, agentsSubdir))
	}
	return dirs
}

// loadMarkdownAgents scans the given directories (nil selects the defaults)
// and returns the valid named agents, first match winning on a name collision.
// knownTools is the set of tool names the subagent process can actually have;
// anything else in a tools list is dropped with a warning. Invalid files are
// skipped with a warning - they never fail startup.
func loadMarkdownAgents(dirs []string, knownTools map[string]bool) []markdownAgent {
	if dirs == nil {
		dirs = markdownAgentDirs()
	}
	seen := make(map[string]bool)
	var agents []markdownAgent
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Warn("failed to read agents directory", "path", dir, "error", err)
			}
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			agent, reason := loadMarkdownAgent(path, knownTools)
			if reason != "" {
				logger.Warn("skipping invalid markdown agent", "file", path, "reason", reason)
				continue
			}
			agent.source = subagentSourceLabel(dir)
			if seen[agent.name] {
				logger.Debug("markdown agent name already loaded from higher-priority directory, skipping", "name", agent.name, "file", path)
				continue
			}
			seen[agent.name] = true
			agents = append(agents, *agent)
		}
	}
	return agents
}

// loadMarkdownAgent parses one Markdown agent file, returning (agent, "") on
// success or (nil, reason) when the file is invalid and must be skipped.
func loadMarkdownAgent(path string, knownTools map[string]bool) (*markdownAgent, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Sprintf("read failed: %v", err)
	}

	fmBlock, body, err := skills.SplitFrontmatter(data)
	if err != nil {
		return nil, err.Error()
	}

	var fm markdownAgentFrontmatter
	if err := yaml.Unmarshal([]byte(fmBlock), &fm); err != nil {
		return nil, fmt.Sprintf("invalid YAML in frontmatter: %v", err)
	}

	fm.Name = strings.TrimSpace(fm.Name)
	if fm.Name == "" {
		return nil, "`name` is required in frontmatter"
	}
	if len(fm.Name) > markdownAgentNameMaxLen {
		return nil, fmt.Sprintf("`name` is %d chars (max %d)", len(fm.Name), markdownAgentNameMaxLen)
	}
	if !markdownAgentNameRegex.MatchString(fm.Name) {
		return nil, fmt.Sprintf("`name` must match %s (lowercase letters, digits, hyphens, underscores)", markdownAgentNameRegex.String())
	}
	if strings.TrimSpace(fm.Description) == "" {
		return nil, "`description` is required in frontmatter"
	}

	tools, reason := resolveMarkdownAgentTools(fm, path, knownTools)
	if reason != "" {
		return nil, reason
	}

	return &markdownAgent{
		name:         fm.Name,
		description:  strings.TrimSpace(fm.Description),
		model:        resolveMarkdownAgentModel(fm.Model, path),
		tools:        tools,
		systemPrompt: strings.TrimSpace(body),
		path:         path,
	}, ""
}

// resolveMarkdownAgentModel validates the frontmatter model: empty or
// "inherit" means inherit, a provider-prefixed value is kept as-is, and
// anything else (Claude aliases like "sonnet", bare model ids) warns and
// falls back to inherit so foreign agent files still load.
func resolveMarkdownAgentModel(model, path string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.EqualFold(model, "inherit") {
		return ""
	}
	if !strings.Contains(model, "/") {
		logger.Warn("markdown agent model has no provider prefix, falling back to inherit", "file", path, "model", model)
		return ""
	}
	return model
}

// resolveMarkdownAgentTools computes the tool allowlist: an explicit `tools`
// list (or the full known set when only `disallowedTools` is given) minus
// unknown names and the disallowed set. A restriction that resolves to no
// tool is a broken definition and skips the file; with neither key set the
// agent inherits the parent's tools (nil allowlist).
func resolveMarkdownAgentTools(fm markdownAgentFrontmatter, path string, knownTools map[string]bool) ([]string, string) {
	requested := flexStringList(fm.Tools)
	disallowed := make(map[string]bool)
	for _, name := range flexStringList(fm.DisallowedTools) {
		disallowed[name] = true
	}
	if requested == nil && len(disallowed) == 0 {
		return nil, ""
	}

	if requested == nil {
		for name := range knownTools {
			requested = append(requested, name)
		}
	}

	var resolved []string
	dropped := make(map[string]bool)
	for _, name := range requested {
		switch {
		case !knownTools[name]:
			if !dropped[name] {
				dropped[name] = true
				logger.Warn("markdown agent requests unknown tool, dropping it", "file", path, "tool", name)
			}
		case disallowed[name]:
		default:
			resolved = append(resolved, name)
		}
	}

	if len(resolved) == 0 {
		return nil, "tools resolve to no known tool (an empty allowlist never falls back to all tools)"
	}
	sort.Strings(resolved)
	return resolved, ""
}

// flexStringList accepts a YAML list (Gemini CLI) or a comma-separated string
// (Claude Code) and returns the trimmed entries.
func flexStringList(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		var out []string
		for _, item := range strings.Split(t, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	default:
		return nil
	}
}

// subagentSourceLabel classifies an agents directory as "user" (the home
// config dir) or "project", for user-facing listings. The project dir is
// cwd-relative, so both sides are normalized to absolute paths before the
// comparison - otherwise filepath.Rel errors and everything reads as "user".
func subagentSourceLabel(dir string) string {
	if home, err := os.UserHomeDir(); err == nil {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		if same, _ := filepath.Rel(filepath.Join(home, config.ConfigDirName), dir); same == "." || !strings.HasPrefix(same, "..") {
			return "user"
		}
	}
	return "project"
}

// subagentInfo renders the agent as the shared SubagentInfo DTO for the
// /agents listing. With no allowlist the subagent inherits the parent's tools
// and runs ReadWrite, so ReadOnly is only true for a resolved read-only list.
func (m markdownAgent) subagentInfo() agentdomain.SubagentInfo {
	return agentdomain.SubagentInfo{
		Name:        m.name,
		Description: m.description,
		Model:       m.model,
		Tools:       m.tools,
		ReadOnly:    len(m.tools) > 0 && deriveSubagentMode(m.tools) == agentdomain.AgentModeReadOnly,
		Source:      m.source,
	}
}

// deriveSubagentMode maps a resolved tool allowlist to a capability mode:
// ReadOnly only when every allowed tool is read-only, else ReadWrite.
func deriveSubagentMode(tools []string) agentdomain.AgentMode {
	for _, name := range tools {
		if !agentdomain.ReadOnlyTools[name] {
			return agentdomain.AgentModeStandard
		}
	}
	return agentdomain.AgentModeReadOnly
}
