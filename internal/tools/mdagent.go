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

// markdownAgentNameMax mirrors the 64-char cap used by Claude Code and skills.
const markdownAgentNameMaxLen = 64

// markdownAgentNameRegex enforces the lowercase letters/digits/hyphen/underscore
// charset accepted for named agents.
var markdownAgentNameRegex = regexp.MustCompile(`^[a-z0-9_-]+$`)

// markdownAgentRoleMaxLen bounds a role to a job title, "code reviewer", so it
// fits in brackets after the name wherever the agent is listed.
const markdownAgentRoleMaxLen = 40

// ValidAgentName reports whether name may name an agent: lowercase letters,
// digits, hyphens and underscores, at most markdownAgentNameMaxLen of them.
func ValidAgentName(name string) bool {
	return name != "" && len(name) <= markdownAgentNameMaxLen && markdownAgentNameRegex.MatchString(name)
}

// markdownAgent is a subagent preset defined as a Markdown file with YAML
// frontmatter: the body is the system prompt. disallowedTools is applied by the
// worker, mcpServers nil means the enabled ones, workspace is the computer a
// teammate spawned from it runs on, role the job title listings show after the
// name, and source is "project" or "user" for listings.
type markdownAgent struct {
	name            string
	role            string
	description     string
	model           string
	workspace       string
	tools           []string
	disallowedTools []string
	mcpServers      []string
	systemPrompt    string
	path            string
	source          string
}

// markdownAgentFrontmatter is deliberately permissive: only the keys we honor
// are declared, everything else (color, temperature, max_turns,
// permissionMode, ...) is accepted and ignored for vendor-file portability.
type markdownAgentFrontmatter struct {
	Name            string `yaml:"name"`
	Role            string `yaml:"role"`
	Description     string `yaml:"description"`
	Model           string `yaml:"model"`
	Workspace       string `yaml:"workspace"`
	Tools           any    `yaml:"tools"`
	DisallowedTools any    `yaml:"disallowedTools"`
	MCPServers      any    `yaml:"mcpServers"`
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
		filepath.Join(root, config.ConfigDirName, config.MarkdownAgentsDirName),
		filepath.Join(root, config.AgentsDirName, config.MarkdownAgentsDirName),
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
	return parseMarkdownAgent(data, path, knownTools)
}

// parseMarkdownAgent parses the content of one Markdown agent, found at path,
// returning (agent, "") on success or (nil, reason) when it must be skipped.
func parseMarkdownAgent(data []byte, path string, knownTools map[string]bool) (*markdownAgent, string) {
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
		role:            resolveMarkdownAgentRole(fm.Role, path),
		description:     strings.TrimSpace(fm.Description),
		model:           resolveMarkdownAgentModel(fm.Model, path),
		workspace:       strings.TrimSpace(fm.Workspace),
		tools:           tools,
		disallowedTools: flexStringList(fm.DisallowedTools),
		mcpServers:      resolveMarkdownAgentMCPServers(fm.MCPServers, path),
		systemPrompt:    strings.TrimSpace(body),
		path:            path,
	}, ""
}

// resolveMarkdownAgentRole trims a role and drops one too long for a job
// title, with a warning, so the file still loads.
func resolveMarkdownAgentRole(role, path string) string {
	role = strings.TrimSpace(role)
	if len(role) > markdownAgentRoleMaxLen {
		logger.Warn("markdown agent role dropped, too long", "file", path, "max", markdownAgentRoleMaxLen)
		return ""
	}
	return role
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

// ResolveMainAgent resolves the named Markdown agent, from projectDir's agent
// dirs, the user's, then the built-ins, into the main agent a chat switches to.
// The tool lists and MCP servers stay unresolved, as for a thread's agent.
func ResolveMainAgent(projectDir, name string) (agentdomain.MainAgent, error) {
	agent, err := findMarkdownAgent(loadMarkdownAgentsWithBuiltins(markdownAgentDirs(projectDir), nil), name)
	if err != nil {
		return agentdomain.MainAgent{}, err
	}
	return agentdomain.MainAgent{
		Name:            agent.name,
		Instructions:    agent.systemPrompt,
		Model:           agent.model,
		Tools:           agent.tools,
		DisallowedTools: agent.disallowedTools,
		MCPServers:      agent.mcpServers,
	}, nil
}

// ResolveThreadAgent overlays the Markdown agent a daemon thread names, from
// projectDir's .infer/agents, ~/.infer/agents, then the built-ins, onto the thread's options,
// and records the agent in the session's origin. The body is the system prompt and a
// frontmatter model wins over the thread's. The tool lists and MCP servers stay
// unresolved for the worker, which checks them against what its computer has.
func ResolveThreadAgent(projectDir string, opts sessionsdomain.ThreadOptions) (sessionsdomain.ThreadOptions, error) {
	agent, err := findMarkdownAgent(loadMarkdownAgentsWithBuiltins(markdownAgentDirs(projectDir), nil), opts.Agent)
	if err != nil {
		return sessionsdomain.ThreadOptions{}, err
	}
	opts.SystemPrompt = agent.systemPrompt
	if agent.model != "" {
		opts.Model = agent.model
	}
	opts.Tools = agent.tools
	opts.DisallowedTools = agent.disallowedTools
	opts.MCPServers = agent.mcpServers
	opts.Origin.Agent = agent.name
	return opts, nil
}

// ProjectAgents lists the agents a thread in projectDir can run as, found the
// way ResolveThreadAgent finds them: the project's and the user's Markdown
// agents and the built-ins.
func ProjectAgents(projectDir string) []agentdomain.SubagentInfo {
	agents := loadMarkdownAgentsWithBuiltins(markdownAgentDirs(projectDir), nil)
	infos := make([]agentdomain.SubagentInfo, 0, len(agents))
	for _, agent := range agents {
		infos = append(infos, agentdomain.SubagentInfo{Name: agent.name, Role: agent.role, Description: agent.description, Model: agent.model, Source: agent.source, BuiltIn: isBuiltinAgent(agent.name), Workspace: agent.workspace})
	}
	return infos
}

// resolveMarkdownAgentMCPServers reads mcpServers as names of servers in the
// mcp.yaml of the computer a thread runs on. An inline definition, which Claude
// Code also accepts, is ignored with a warning, since agent files travel and
// must never carry a URL, a header or a secret. Nil keeps the enabled servers.
func resolveMarkdownAgentMCPServers(v any, path string) []string {
	if v == nil {
		return nil
	}
	items, isList := v.([]any)
	if !isList {
		items = []any{v}
	}
	names := []string{}
	for _, item := range items {
		name, isName := item.(string)
		if !isName {
			logger.Warn("markdown agent defines an MCP server inline, ignoring it", "file", path)
			continue
		}
		names = append(names, flexStringList(name)...)
	}
	return names
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
		Role:        m.role,
		Description: m.description,
		Model:       m.model,
		Tools:       m.tools,
		ReadOnly:    len(m.tools) > 0 && deriveSubagentMode(m.tools, tools) == agentdomain.AgentModeReadOnly,
		Source:      m.source,
		BuiltIn:     isBuiltinAgent(m.name),
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
