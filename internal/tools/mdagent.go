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
	sessionsdomain "github.com/inference-gateway/cli/internal/sessions/domain"
	skills "github.com/inference-gateway/cli/internal/skills"
)

// agentsSubdir is the directory (under .infer/ and .agents/, in the project and
// in the home dir) holding Markdown agent definitions, one <name>.md per agent.
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
	name        string
	description string
	model       string
	tools       []string
	// disallowedTools is the frontmatter's list as written, which a thread's
	// worker applies against the tools it has.
	disallowedTools []string
	systemPrompt    string
	path            string
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

// markdownAgentDirs returns the search paths in precedence order, laid out like
// skills and custom tools: the project's .infer/agents and .agents/agents, then
// the user's ~/.infer/agents and ~/.agents/agents, so a project overrides a
// personal agent of the same name. An empty projectDir is the cwd.
func markdownAgentDirs(projectDir string) []string {
	dirs := agentDirsUnder(projectDir)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, agentDirsUnder(home)...)
	}
	return dirs
}

// agentDirsUnder is root's .infer/agents, then its .agents/agents.
func agentDirsUnder(root string) []string {
	return []string{
		filepath.Join(root, config.ConfigDirName, agentsSubdir),
		filepath.Join(root, config.AgentsDirName, agentsSubdir),
	}
}

// loadMarkdownAgents scans the given directories (nil selects the defaults)
// and returns the valid named agents, first match winning on a name collision.
// A tools list keeps only knownTools, the names the agent's process can have,
// and a nil knownTools keeps every name. Invalid files are skipped with a warning.
func loadMarkdownAgents(dirs []string, knownTools map[string]bool) []markdownAgent {
	if dirs == nil {
		dirs = markdownAgentDirs("")
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
		name:            fm.Name,
		description:     strings.TrimSpace(fm.Description),
		model:           resolveMarkdownAgentModel(fm.Model, path),
		tools:           tools,
		disallowedTools: flexStringList(fm.DisallowedTools),
		systemPrompt:    strings.TrimSpace(body),
		path:            path,
	}, ""
}

// findMarkdownAgent returns the named agent, exact-match on the frontmatter
// name (which need not equal the file name), or an error listing the others.
func findMarkdownAgent(agents []markdownAgent, name string) (markdownAgent, error) {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.name == name {
			return agent, nil
		}
		names = append(names, agent.name)
	}
	sort.Strings(names)
	return markdownAgent{}, fmt.Errorf("unknown agent %q; available agents: %s", name, strings.Join(names, ", "))
}

// ResolveThreadAgent overlays the Markdown agent a daemon thread names, from
// projectDir's .infer/agents then ~/.infer/agents, onto the thread's options.
// As for a named subagent, the body is the system prompt and a frontmatter model
// wins, pinned for the thread. The tool lists stay unresolved for the worker.
func ResolveThreadAgent(projectDir string, opts sessionsdomain.ThreadOptions) (sessionsdomain.ThreadOptions, error) {
	agent, err := findMarkdownAgent(loadMarkdownAgents(markdownAgentDirs(projectDir), nil), opts.Agent)
	if err != nil {
		return sessionsdomain.ThreadOptions{}, err
	}
	opts.SystemPrompt = agent.systemPrompt
	if agent.model != "" {
		opts.Model = agent.model
		opts.PinModel = true
	}
	opts.Tools = agent.tools
	opts.DisallowedTools = agent.disallowedTools
	return opts, nil
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

// resolveMarkdownAgentTools computes the tool allowlist: the `tools` list, or
// the known set when only `disallowedTools` is given, minus unknown names and
// the disallowed set. A restriction that resolves to no tool skips the file,
// and nil inherits the parent's tools. A nil knownTools treats every name as
// known and leaves a lone `disallowedTools` to the worker's own tools.
func resolveMarkdownAgentTools(fm markdownAgentFrontmatter, path string, knownTools map[string]bool) ([]string, string) {
	requested := flexStringList(fm.Tools)
	disallowed := make(map[string]bool)
	for _, name := range flexStringList(fm.DisallowedTools) {
		disallowed[name] = true
	}
	if requested == nil && (len(disallowed) == 0 || knownTools == nil) {
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
		case knownTools != nil && !knownTools[name]:
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
		for _, userDir := range agentDirsUnder(home) {
			if same, _ := filepath.Rel(filepath.Dir(userDir), dir); same == "." || !strings.HasPrefix(same, "..") {
				return "user"
			}
		}
	}
	return "project"
}

// subagentInfo renders the agent as the shared SubagentInfo DTO for the
// /agents listing. With no allowlist the subagent inherits the parent's tools
// and runs ReadWrite, so ReadOnly is only true for a resolved read-only list.
func (m markdownAgent) subagentInfo(tools agentdomain.ToolManifestLookup) agentdomain.SubagentInfo {
	return agentdomain.SubagentInfo{
		Name:        m.name,
		Description: m.description,
		Model:       m.model,
		Tools:       m.tools,
		ReadOnly:    len(m.tools) > 0 && deriveSubagentMode(m.tools, tools) == agentdomain.AgentModeReadOnly,
		Source:      m.source,
	}
}

// deriveSubagentMode maps a resolved tool allowlist to a capability mode:
// ReadOnly only when every allowed tool is read-only, else ReadWrite.
func deriveSubagentMode(names []string, tools agentdomain.ToolManifestLookup) agentdomain.AgentMode {
	for _, name := range names {
		if !tools.Manifest(name).AvailableIn(agentdomain.AgentModeReadOnly) {
			return agentdomain.AgentModeStandard
		}
	}
	return agentdomain.AgentModeReadOnly
}
