package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	states "github.com/inference-gateway/cli/internal/agent/states"
	tools "github.com/inference-gateway/cli/internal/agent/tools"
	conversation "github.com/inference-gateway/cli/internal/conversation"
	convdomain "github.com/inference-gateway/cli/internal/conversation/domain"
	constants "github.com/inference-gateway/cli/internal/platform/constants"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	logger "github.com/inference-gateway/cli/internal/platform/logger"
	project "github.com/inference-gateway/cli/internal/platform/project"
	streamevent "github.com/inference-gateway/cli/internal/platform/streamevent"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	plugins "github.com/inference-gateway/cli/internal/plugins"
)

// accumulateToolCalls processes multiple tool call deltas and stores them in the agent's toolCallsMap
func (s *AgentServiceImpl) accumulateToolCalls(deltas []sdk.ChatCompletionMessageToolCallChunk) {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	for _, delta := range deltas {
		key := fmt.Sprintf("%d", delta.Index)

		deltaID := ""
		if delta.ID != nil {
			deltaID = *delta.ID
		}

		if s.toolCallsMap[key] == nil {
			s.toolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
				ID:   deltaID,
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "",
					Arguments: "",
				},
			}
		}

		toolCall := s.toolCallsMap[key]
		if deltaID != "" {
			toolCall.ID = deltaID
		}
		if delta.Function != nil && delta.Function.Name != "" && toolCall.Function.Name == "" {
			toolCall.Function.Name = delta.Function.Name
		}
		if delta.ExtraContent != nil {
			mergeToolCallExtraContent(toolCall, delta.ExtraContent)
		}
		if delta.Function != nil && delta.Function.Arguments != "" {
			if toolCall.Function.Arguments == "" {
				toolCall.Function.Arguments = delta.Function.Arguments
				continue
			}

			if isCompleteJSON(toolCall.Function.Arguments) {
				continue
			}

			toolCall.Function.Arguments += delta.Function.Arguments
		}
	}
}

// mergeToolCallExtraContent copies provider-specific extras (e.g. Google
// Gemini's thought_signature) from a streaming chunk onto the accumulated
// tool call. The signature must be echoed back verbatim on the next request,
// or Google rejects it with HTTP 400 "missing thought_signature".
func mergeToolCallExtraContent(toolCall *sdk.ChatCompletionMessageToolCall, src *sdk.ToolCallExtraContent) {
	if toolCall.ExtraContent == nil {
		toolCall.ExtraContent = &sdk.ToolCallExtraContent{}
	}
	if src.Google != nil {
		if toolCall.ExtraContent.Google == nil {
			toolCall.ExtraContent.Google = &sdk.ToolCallExtraContent_Google{}
		}
		if src.Google.ThoughtSignature != nil && *src.Google.ThoughtSignature != "" {
			toolCall.ExtraContent.Google.ThoughtSignature = src.Google.ThoughtSignature
		}
		for k, v := range src.Google.AdditionalProperties {
			if toolCall.ExtraContent.Google.AdditionalProperties == nil {
				toolCall.ExtraContent.Google.AdditionalProperties = map[string]any{}
			}
			toolCall.ExtraContent.Google.AdditionalProperties[k] = v
		}
	}
}

// getAccumulatedToolCalls returns a sorted slice of all accumulated tool calls and clears the map
func (s *AgentServiceImpl) getAccumulatedToolCalls() []*sdk.ChatCompletionMessageToolCall {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	if len(s.toolCallsMap) == 0 {
		return nil
	}

	indices := make([]int, 0, len(s.toolCallsMap))
	for key := range s.toolCallsMap {
		var idx int
		_, _ = fmt.Sscanf(key, "%d", &idx)
		indices = append(indices, idx)
	}
	slices.Sort(indices)

	result := make([]*sdk.ChatCompletionMessageToolCall, 0, len(s.toolCallsMap))
	for _, idx := range indices {
		key := fmt.Sprintf("%d", idx)
		if tc, ok := s.toolCallsMap[key]; ok {
			result = append(result, tc)
		}
	}

	s.toolCallsMap = make(map[string]*sdk.ChatCompletionMessageToolCall)
	return result
}

// clearToolCallsMap resets the tool calls map for the next iteration
func (s *AgentServiceImpl) clearToolCallsMap() {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	s.toolCallsMap = make(map[string]*sdk.ChatCompletionMessageToolCall)
}

// BuildSystemPrompt assembles the static system prompt sent as message[0]
// (base prompt + custom instructions + AGENTS.md + plugins + static context).
// It is deliberately byte-identical across turns within a session so local LLM
// servers get KV-cache prefix hits; volatile context (git, tree, memory,
// active skill, date) rides in volatileTailMessage instead. Returns "" when no
// base prompt is configured for the current mode.
func (s *AgentServiceImpl) BuildSystemPrompt() string {
	baseSystemPrompt := s.getSystemPromptForMode()
	if baseSystemPrompt == "" {
		return ""
	}

	agentConfig := s.config.GetAgentConfig()
	parts := []string{baseSystemPrompt}

	if s.config.Prompts.Agent.CustomInstructions != "" {
		parts = append(parts, s.config.Prompts.Agent.CustomInstructions)
	}

	if info := s.buildAgentsMDInfo(); info != "" {
		parts = append(parts, info)
	}

	if block := plugins.InstructionsBlock(s.config); block != "" {
		parts = append(parts, block)
	}

	if agentConfig.SystemPromptWithDefaults {
		contextInfo := s.buildContextInfo()
		if contextInfo != "" {
			parts = append(parts, contextInfo)
		}
	}

	return strings.Join(parts, "\n\n")
}

// VolatileTailText renders the volatile-context tail a fresh session (turn 0)
// would send as a hidden per-request <system-reminder> user message; ok=false
// means no tail is sent. Exposed for the `infer debug agent system_prompt`
// command via type assertion, alongside SystemPromptSections.
func (s *AgentServiceImpl) VolatileTailText() (string, bool) {
	tail, ok := s.volatileTailMessage(nil, true)
	if !ok {
		return "", false
	}
	content, err := tail.Content.AsMessageContent0()
	if err != nil {
		return "", false
	}
	return content, true
}

// addSystemPrompt prepends the assembled system prompt to messages.
func (s *AgentServiceImpl) addSystemPrompt(messages []sdk.Message) []sdk.Message {
	prompt := s.BuildSystemPrompt()
	if prompt == "" {
		return messages
	}

	systemMessage := sdk.Message{
		Role:    sdk.System,
		Content: sdk.NewMessageContent(prompt),
	}

	return append([]sdk.Message{systemMessage}, messages...)
}

// volatileTailMessage builds the per-request <system-reminder> user message
// carrying the volatile context (git, tree, active skill, memory, current
// date), appended to the outbound payload only — never persisted — so the
// system prompt at message[0] stays byte-stable for KV-cache prefix reuse.
// Called once per outbound request; messages is the conversation being sent
// (turn ≈ len/2, driving the turn-window caches of the expensive sections);
// ok=false means append nothing. Callers gate the append on
// conversationAwaitsToolResults at payload-finalization time
// (outboundConversation for streaming, Run for sync), after conversation
// repair — not here, where the input may still carry orphaned tool_calls.
func (s *AgentServiceImpl) volatileTailMessage(messages []sdk.Message, isChat bool) (sdk.Message, bool) {
	if s.getSystemPromptForMode() == "" {
		return sdk.Message{}, false
	}

	var b strings.Builder
	if s.config.GetAgentConfig().SystemPromptWithDefaults {
		for _, section := range s.volatileContextSections(len(messages)/2, messages, isChat) {
			b.WriteString(section.Text)
		}
	}
	fmt.Fprintf(&b, "\n\nCurrent date: %s", time.Now().Format("Monday, January 2, 2006"))

	return sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent("<system-reminder>\n" + strings.TrimSpace(b.String()) + "\n</system-reminder>"),
	}, true
}

// PromptSection is one labeled part of the assembled prompt context, exposed
// for diagnostics (e.g. per-section token estimates in `infer debug`). Text is
// the raw part as it appears in the prompt, including any separator prefix.
// Volatile marks sections delivered via the per-request tail message rather
// than the static system prompt.
type PromptSection struct {
	Name     string
	Text     string
	Volatile bool
}

// contextSections lists the static context builders in prompt order; it is
// the single source for both prompt assembly and diagnostics. Only sections
// that are stable across turns belong here — anything that changes as the
// agent works goes in volatileContextSections so message[0] stays byte-stable.
func (s *AgentServiceImpl) contextSections() []PromptSection {
	return []PromptSection{
		{Name: "sandbox", Text: s.buildSandboxInfo()},
		{Name: "a2a_agents", Text: s.buildA2AAgentInfo()},
		{Name: "os", Text: s.buildOSInfo()},
		{Name: "working_directory", Text: s.buildWorkingDirectoryInfo()},
		{Name: "github_guidance", Text: s.buildGitHubGuidanceInfo()},
		{Name: "bash_allow_list", Text: s.buildBashAllowInfo()},
		{Name: "tools", Text: s.buildToolsInfo()},
		{Name: "skills", Text: s.buildSkillsInfo()},
	}
}

// volatileContextSections lists the context builders whose output changes as
// the session progresses; they are delivered per request via
// volatileTailMessage rather than in the system prompt.
func (s *AgentServiceImpl) volatileContextSections(currentTurn int, messages []sdk.Message, isChat bool) []PromptSection {
	return []PromptSection{
		{Name: "git_context", Text: s.buildGitContextInfo(currentTurn), Volatile: true},
		{Name: "project_structure", Text: s.buildProjectTreeInfo(currentTurn), Volatile: true},
		{Name: "active_skill", Text: s.buildActiveSkillInfo(messages, isChat), Volatile: true},
		{Name: "memory", Text: s.buildMemoryInfo(currentTurn), Volatile: true},
	}
}

// buildContextInfo assembles the static context (sandbox, A2A, OS, working dir, GitHub, tools, skills) for the system prompt
func (s *AgentServiceImpl) buildContextInfo() string {
	var b strings.Builder
	for _, section := range s.contextSections() {
		b.WriteString(section.Text)
	}
	return b.String()
}

// SystemPromptSections returns the labeled parts of the prompt context a fresh
// session (turn 0) would send — the static system prompt sections followed by
// the Volatile-marked tail sections — with empty parts omitted. Exposed for
// the `infer debug agent system_prompt --tokens` breakdown.
func (s *AgentServiceImpl) SystemPromptSections() []PromptSection {
	agentConfig := s.config.GetAgentConfig()
	sections := []PromptSection{
		{Name: "base_prompt", Text: s.getSystemPromptForMode()},
		{Name: "custom_instructions", Text: s.config.Prompts.Agent.CustomInstructions},
		{Name: "agents_md", Text: s.buildAgentsMDInfo()},
		{Name: "plugins", Text: plugins.InstructionsBlock(s.config)},
	}
	if agentConfig.SystemPromptWithDefaults {
		sections = append(sections, s.contextSections()...)
		sections = append(sections, s.volatileContextSections(0, nil, true)...)
	}

	nonEmpty := sections[:0]
	for _, section := range sections {
		if section.Text != "" {
			nonEmpty = append(nonEmpty, section)
		}
	}
	return nonEmpty
}

// buildGitHubGuidanceInfo steers the model toward the `gh` CLI for GitHub work
// and away from shell habits that needlessly trip the Bash allowed list. There is
// no built-in GitHub tool; `gh` (via Bash) covers issues, PRs, releases, and the
// raw API with clearer errors and the standard credential chain. Emitted only
// when Bash is enabled (otherwise the guidance is moot). Lives in the dynamic
// context so it reaches existing users regardless of their prompts.yaml override.
func (s *AgentServiceImpl) buildGitHubGuidanceInfo() string {
	if !s.config.Tools.Bash.Enabled {
		return ""
	}
	return "\n\nGITHUB OPERATIONS:\n" +
		"Use the `gh` CLI via the Bash tool for GitHub operations - issues, pull requests, " +
		"releases, and repository metadata (e.g. `gh issue view`, `gh pr view`, `gh repo view`). " +
		"There is no built-in GitHub tool. Prefer the structured `gh` subcommands over the raw " +
		"`gh api`: the read-only subcommands are auto-approved, while writes (e.g. `gh issue " +
		"create`, `gh pr create`) and the raw `gh api` need approval unless your config opts " +
		"into them. " +
		"Ensure `gh` is authenticated (it uses the standard gh/GITHUB_TOKEN credential chain).\n\n" +
		"BASH USAGE:\n" +
		"The Bash tool already captures stdout, stderr, and the exit code. Do NOT append `2>&1`, " +
		"`2>/dev/null`, or `|| echo ...` to commands - they are unnecessary and can cause an " +
		"otherwise-allowed command to be rejected. Run ONE command per call: pipes and operators " +
		"(|, &&, ||, ;) are not auto-approved, so run each step as a separate call instead of " +
		"chaining them. The commands auto-approved in the current mode are listed under BASH " +
		"ALLOW-LIST below."
}

// buildBashAllowInfo lists the bash commands auto-approved in the active agent
// mode so the model knows its sandbox up front. It reads the live mode (the same
// one the approval check uses), so toggling auto/plan in chat updates it on the
// next turn; in agent mode it is simply present from the start. Empty when the
// Bash tool is disabled or filtered out of the current mode (e.g. plan mode). An
// unrestricted mode (allow-list ".*") is described in prose rather than dumping a
// meaningless pattern.
func (s *AgentServiceImpl) buildBashAllowInfo() string {
	if !s.config.Tools.Bash.Enabled || s.toolService == nil {
		return ""
	}

	mode := agentdomain.AgentModeStandard
	if s.stateManager != nil {
		mode = s.stateManager.GetAgentMode()
	}

	// Skip when Bash is not callable in this mode (plan mode filters it out), so
	// the prompt never advertises an allow-list for a tool the model cannot use.
	bashAvailable := false
	for _, def := range s.toolService.ListToolsForMode(mode) {
		if def.Function.Name == "Bash" {
			bashAvailable = true
			break
		}
	}
	if !bashAvailable {
		return ""
	}

	modeKey := mode.AllowedlistKey()
	allow := s.config.BashAllowedCommands(modeKey)

	header := "\n\nBASH ALLOW-LIST (" + modeKey + " mode):\n"

	for _, e := range allow {
		switch strings.TrimSpace(e) {
		case ".*", "^.*$", "^.*", ".*$", ".+", "^.+$", "^.+", ".+$":
			return header + "This mode is unrestricted: any command runs via Bash without " +
				"approval, including pipes, chains, redirects, and command substitution. " +
				"Prefer one command per call for clear output, and never echo or publish a secret.\n"
		}
	}

	if len(allow) == 0 {
		return header + "No commands are auto-approved in this mode; every Bash command " +
			"requires approval.\n"
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteString("These command patterns (regular expressions, matched against the WHOLE " +
		"command) run without approval. Anything else requires approval (chat) or is rejected " +
		"(agent mode). Run ONE command per call:\n")
	for _, e := range allow {
		fmt.Fprintf(&b, "- %s\n", e)
	}
	b.WriteString("\nIf a command you need is NOT covered above, it will be denied (agent " +
		"mode) or require approval (chat). Do NOT retry the same or a trivially reworded command " +
		"hoping it passes - it will keep being denied. Instead, accomplish the goal with an " +
		"allowed command, or stop and tell the user exactly what you need to run and why.\n")
	return b.String()
}

// buildToolsInfo lists the tools available to the model for the active agent
// mode as a lightweight name + one-line-description roster. The list is derived
// from the same toolService.ListToolsForMode(mode) call that populates the
// request's native tool definitions, so the prose can never drift from what the
// model can actually call. Empty when tools are disabled or none are registered
// (e.g. NoOpToolService, or before MCP tools finish async registration).
func (s *AgentServiceImpl) buildToolsInfo() string {
	if s.toolService == nil {
		return ""
	}

	mode := agentdomain.AgentModeStandard
	if s.stateManager != nil {
		mode = s.stateManager.GetAgentMode()
	}

	defs := s.toolService.ListToolsForMode(mode)
	if len(defs) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nAVAILABLE TOOLS:\n")
	b.WriteString("These are the tools you can call right now (full parameter " +
		"schemas are supplied separately via the tool-use API). Use the exact name:\n")
	for _, def := range defs {
		desc := ""
		if def.Function.Description != nil {
			desc = formatting.TruncateText(strings.SplitN(*def.Function.Description, "\n", 2)[0], 100)
		}
		if desc != "" {
			fmt.Fprintf(&b, "- %s: %s\n", def.Function.Name, desc)
		} else {
			fmt.Fprintf(&b, "- %s\n", def.Function.Name)
		}
	}
	return b.String()
}

// getSystemPromptForMode returns the appropriate system prompt based on current agent mode
func (s *AgentServiceImpl) getSystemPromptForMode() string {
	prompts := s.config.Prompts.Agent

	if s.stateManager == nil {
		return prompts.SystemPrompt
	}

	mode := s.stateManager.GetAgentMode()
	switch mode {
	case agentdomain.AgentModePlan:
		if prompts.SystemPromptPlan != "" {
			return prompts.SystemPromptPlan
		}
		return prompts.SystemPrompt

	case agentdomain.AgentModeAutoAccept:
		if prompts.SystemPromptAuto != "" {
			return prompts.SystemPromptAuto
		}
		return prompts.SystemPrompt

	case agentdomain.AgentModeStandard:
		return prompts.SystemPrompt

	case agentdomain.AgentModeReadOnly:
		return prompts.SystemPrompt

	default:
		return prompts.SystemPrompt
	}
}

// buildSkillsInfo lists discovered Agent Skills with their absolute SKILL.md
// paths so the model can read each one on demand via the Read tool.
// Empty when skills are disabled or none were discovered.
//
// Catalog skills are listed WITHOUT a path, even after their body has been
// downloaded. This section rides in the static system prompt, so rendering the
// mutable Path here would rewrite message[0] the moment a skill is installed
// and invalidate the whole conversation's KV-cache prefix. The concrete path is
// delivered by buildActiveSkillInfo in the volatile tail instead, which is
// per-request by design.
func (s *AgentServiceImpl) buildSkillsInfo() string {
	if s.skillsService == nil {
		return ""
	}
	skills := s.skillsService.List()
	if len(skills) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nAVAILABLE SKILLS:\n")
	b.WriteString("Skills are reusable instructions for specific tasks. ")
	b.WriteString("When a task matches a skill's description, read the SKILL.md file at the listed path using the Read tool, then follow its instructions. ")
	b.WriteString("To deterministically load a skill's full instructions, invoke it explicitly with /<name> or \"use the <name> skill\".\n\n")

	var maxChars int
	if s.config != nil {
		maxChars = s.config.GetAgentConfig().Skills.MaxChars
	}
	// Expand path-bearing skills before catalog ones: those are the entries the
	// model can act on with the Read tool, whereas a catalog entry is just a
	// name only the user can invoke. Under budget pressure the latter is what
	// should drop. skills is List()'s defensive copy, so sorting is local.
	sort.SliceStable(skills, func(i, j int) bool {
		return skills[i].Scope != agentdomain.SkillScopeCatalog && skills[j].Scope == agentdomain.SkillScopeCatalog
	})

	var omitted int
	for i, sk := range skills {
		location := fmt.Sprintf("Path: %s", sk.Path)
		if sk.Scope == agentdomain.SkillScopeCatalog {
			location = fmt.Sprintf("Installed from the catalog on demand - invoke /%s to activate it", sk.DisplayName())
		}
		entry := fmt.Sprintf("- %s (%s): %s\n  %s\n", sk.DisplayName(), sk.Scope, sk.Description, location)
		if maxChars > 0 && b.Len()+len(entry) > maxChars {
			omitted = len(skills) - i
			break
		}
		b.WriteString(entry)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "... (%d more skills not expanded - run `infer skills search <term>` to find one)\n", omitted)
	}

	return b.String()
}

var (
	// skillNameLead extracts the leading skill-name run from a "/<name>" token
	// after the slash (names are the lowercase [a-z0-9-]+ charset enforced at
	// load time). Applied per whitespace-delimited field so adjacent tokens
	// like "/foo /bar" both match.
	skillNameLead = regexp.MustCompile(`^[a-z0-9-]+`)
	// skillPluginRef matches "/plugin-name:skill-name" syntax for referencing
	// a skill from a specific plugin. Both plugin and skill names use the
	// lowercase [a-z0-9-]+ charset.
	skillPluginRef = regexp.MustCompile(`^([a-z0-9-]+):([a-z0-9-]+)$`)
	// skillPhraseTrigger matches natural-language "use the <name> skill" /
	// "use <name> skill" (case-insensitive). The boundaries are zero-width, so
	// consecutive phrases don't shadow one another.
	skillPhraseTrigger = regexp.MustCompile(`(?i)\buse(?:\s+the)?\s+([a-z0-9-]+)\s+skill\b`)
)

// buildActiveSkillInfo deterministically surfaces the skills the user
// explicitly invoked (via "/<name>" or "use the <name> skill"). Unlike
// buildSkillsInfo - which lists every available skill and leaves it to the
// model whether to engage one - this guarantees an invoked skill is flagged as
// active regardless of mode (chat, `infer headless`, channels, heartbeat), all of
// which funnel through addSystemPrompt. It injects only the skill's metadata
// (description + path), not the body: the SKILL.md body stays progressive
// disclosure, read on demand by the model via the Read tool (now reachable
// thanks to the sandbox carve-out). Empty when no trigger matched a loaded
// skill.
//
// A catalog skill that has not been downloaded yet (empty Path) is installed
// on the spot when isChat is false - headless runs have nobody to ask. In chat
// the install is user-approved at the input layer instead (see
// ChatMessageProcessor.confirmCatalogInstall), so a not-yet-installed skill is
// skipped here rather than fetched behind the user's back.
func (s *AgentServiceImpl) buildActiveSkillInfo(messages []sdk.Message, isChat bool) string {
	if s.skillsService == nil {
		return ""
	}

	names := s.matchSkillTriggers(messages)
	if len(names) == 0 {
		return ""
	}

	var entries []string
	for _, name := range names {
		sk, ok := s.skillsService.Get(name)
		if !ok {
			continue
		}
		if sk.Path == "" {
			if isChat {
				continue
			}
			if sk, ok = s.skillsService.Discover(context.Background(), name); !ok {
				continue
			}
		}
		entries = append(entries, fmt.Sprintf("- %s (%s): %s\n  Path: %s", sk.DisplayName(), sk.Scope, sk.Description, sk.Path))
	}

	if len(entries) == 0 {
		return ""
	}

	header := "ACTIVE SKILL"
	if len(entries) > 1 {
		header = "ACTIVE SKILLS"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "\n\n%s - the user explicitly invoked the following; read each SKILL.md at the listed path with the Read tool and follow its instructions:\n", header)
	b.WriteString(strings.Join(entries, "\n"))
	b.WriteString("\n")
	return b.String()
}

// buildAgentsMDInfo injects the project-root AGENTS.md into the system
// prompt, appended after custom instructions. Returns "" when the file is
// missing/unreadable or agent.agents_md.enabled is false.
func (s *AgentServiceImpl) buildAgentsMDInfo() string {
	if s.config == nil || !s.config.Agent.AgentsMD.Enabled {
		return ""
	}

	data, err := os.ReadFile("AGENTS.md")
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debug("failed to read project AGENTS.md", "error", err)
		}
		return ""
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return ""
	}

	content, marker := plugins.CapInstructions(content, s.config.Agent.AgentsMD.MaxLines, s.config.Agent.AgentsMD.MaxChars)
	if marker != "" {
		content += "\n" + marker
	}

	return "PROJECT INSTRUCTIONS (AGENTS.md):\n" + content
}

// buildMemoryInfo loads the MEMORY.md index once per session and injects it as a
// context block so the agent knows which durable facts exist; individual facts
// are loaded on demand via the Memory tool. Cached like gitContextCache; the
// per-turn TTL means writes from the prior turn are reflected on the next turn.
func (s *AgentServiceImpl) buildMemoryInfo(currentTurn int) string {
	if !s.config.Memory.Enabled {
		return ""
	}

	s.contextCacheMux.RLock()
	if s.memoryContextCache != "" && currentTurn-s.memoryContextTurn < 1 {
		defer s.contextCacheMux.RUnlock()
		return s.memoryContextCache
	}
	s.contextCacheMux.RUnlock()

	dir, err := s.config.ResolveMemoryDir()
	if err != nil {
		return ""
	}

	indexPath := filepath.Join(dir, config.MemoryIndexFileName)
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Debug("failed to read memory index", "path", indexPath, "error", err)
		}
		return ""
	}

	index := strings.TrimSpace(string(data))
	if index == "" {
		return ""
	}
	index = filterMemoryIndex(index, project.Detect().Slug)
	if maxChars := s.config.Memory.MaxChars; maxChars > 0 && len(index) > maxChars {
		cut := index[:maxChars]
		if nl := strings.LastIndexByte(cut, '\n'); nl > 0 {
			cut = cut[:nl]
		}
		index = cut + "\n... (memory index truncated; use the Memory tool 'read' with a name for full facts)"
	}

	var b strings.Builder
	b.WriteString("\n\nPERSISTENT MEMORY INDEX (global facts plus facts for the current project, from past sessions; use the Memory tool 'read' with a name to load one in full):\n")
	b.WriteString(index)
	b.WriteString("\n")

	result := b.String()

	s.contextCacheMux.Lock()
	s.memoryContextCache = result
	s.memoryContextTurn = currentTurn
	s.contextCacheMux.Unlock()

	return result
}

// memoryIndexLink extracts the link target from a MEMORY.md entry line.
var memoryIndexLink = regexp.MustCompile(`\]\(([^)]+)\.md\)`)

// filterMemoryIndex keeps index entry lines that are global (root-level link)
// or belong to projectSlug, and collapses every other project into one summary
// line so the injected index stays small while other projects remain
// discoverable. Non-entry lines (header, blanks) and unparsable entry lines
// are kept as-is (fail open).
func filterMemoryIndex(index, projectSlug string) string {
	var b strings.Builder
	others := make(map[string]struct{})

	for line := range strings.SplitSeq(index, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}
		m := memoryIndexLink.FindStringSubmatch(trimmed)
		if m == nil {
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}
		lineProject := ""
		if i := strings.IndexByte(m[1], '/'); i >= 0 {
			lineProject = m[1][:i]
		}
		if lineProject == "" || lineProject == projectSlug {
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}
		others[lineProject] = struct{}{}
	}

	out := strings.TrimRight(b.String(), "\n")
	if len(others) > 0 {
		names := make([]string, 0, len(others))
		for p := range others {
			names = append(names, p)
		}
		sort.Strings(names)
		out += fmt.Sprintf("\n- other projects with memories (not shown; read with \"<project>/<name>\"): %s", strings.Join(names, ", "))
	}
	return out
}

// matchSkillTriggers scans user-role messages for explicit skill invocations
// (slash token, "/plugin:skill" ref, or "use the X skill" phrase), returning
// the de-duplicated names of skills that are actually loaded, in first-seen
// order. Unknown tokens are ignored so a bare "/word" in prose never errors.
func (s *AgentServiceImpl) matchSkillTriggers(messages []sdk.Message) []string {
	seen := make(map[string]struct{})
	var names []string

	add := func(name string) {
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		if _, ok := s.skillsService.Get(name); !ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	for _, msg := range messages {
		if msg.Role != sdk.User {
			continue
		}
		text, err := msg.Content.AsMessageContent0()
		if err != nil || text == "" {
			continue
		}
		for _, field := range strings.Fields(text) {
			if name, ok := strings.CutPrefix(field, "/"); ok {
				lower := strings.ToLower(name)
				if m := skillPluginRef.FindStringSubmatch(lower); m != nil {
					add(m[2])
				} else {
					add(skillNameLead.FindString(lower))
				}
			}
		}
		for _, m := range skillPhraseTrigger.FindAllStringSubmatch(text, -1) {
			add(strings.ToLower(m[1]))
		}
	}

	return names
}

// buildA2AAgentInfo creates dynamic A2A agent information for the system prompt
func (s *AgentServiceImpl) buildA2AAgentInfo() string {
	if s.a2aAgentService == nil {
		return ""
	}

	urls := s.a2aAgentService.GetConfiguredAgents()
	if len(urls) == 0 {
		return ""
	}

	agentInfo := "\n\nAvailable A2A Agents:\n"
	for _, url := range urls {
		agentInfo += fmt.Sprintf("- %s\n", url)
	}
	agentInfo += "\nYou can delegate tasks to these agents using the A2A_SubmitTask tool."
	return agentInfo
}

// buildSandboxInfo creates dynamic sandbox information for the system prompt
func (s *AgentServiceImpl) buildSandboxInfo() string {
	sandboxDirs := s.config.GetSandboxDirectories()
	protectedPaths := s.config.GetProtectedPaths()

	var sandboxInfo strings.Builder
	sandboxInfo.WriteString("SANDBOX RESTRICTIONS:\n")

	if len(sandboxDirs) > 0 {
		sandboxInfo.WriteString("You are restricted to work within these allowed directories:\n")
		for _, dir := range sandboxDirs {
			fmt.Fprintf(&sandboxInfo, "- %s\n", dir)
		}
		sandboxInfo.WriteString("\n")
	}

	if len(protectedPaths) > 0 {
		sandboxInfo.WriteString("You MUST NOT attempt to access these protected paths:\n")
		for _, path := range protectedPaths {
			fmt.Fprintf(&sandboxInfo, "- %s\n", path)
		}
	}

	return sandboxInfo.String()
}

// buildOSInfo creates dynamic OS information for the system prompt
func (s *AgentServiceImpl) buildOSInfo() string {
	osInfo := fmt.Sprintf("\n\nOPERATING SYSTEM: %s", runtime.GOOS)

	switch runtime.GOOS {
	case "darwin":
		osInfo += "\n- Keyboard shortcuts use the 'cmd' modifier (e.g. 'cmd+c')."
		osInfo += "\n- Open apps and files with 'open' (e.g. 'open -a Firefox', 'open file.txt')."
	case "linux":
		osInfo += "\n- Keyboard shortcuts use the 'ctrl' modifier (e.g. 'ctrl+c')."
		osInfo += "\n- Open apps and files with 'xdg-open' or the command name."
	case "windows":
		osInfo += "\n- Keyboard shortcuts use the 'ctrl' modifier (e.g. 'ctrl+c')."
		osInfo += "\n- Open apps and files with 'start' (e.g. 'start notepad.exe', 'start file.txt')."
	}

	return osInfo
}

// buildWorkingDirectoryInfo creates dynamic working directory information for the system prompt
func (s *AgentServiceImpl) buildWorkingDirectoryInfo() string {
	cfg := s.config.GetAgentConfig()
	if !cfg.Context.WorkingDirEnabled {
		return ""
	}

	workingDir, err := os.Getwd()
	if err != nil {
		logger.Debug("failed to get working directory", "error", err)
		return ""
	}

	return fmt.Sprintf("\n\nWORKING DIRECTORY: %s", workingDir)
}

// buildGitContextInfo creates dynamic git repository information for the system
// prompt. The cache is keyed on the current branch as well as the turn window:
// a checkout mid-run invalidates it immediately (at the cost of one cheap
// `git branch --show-current` per request), so the model never reads a stale
// "Current branch" line after switching branches.
func (s *AgentServiceImpl) buildGitContextInfo(currentTurn int) string {
	cfg := s.config.GetAgentConfig()
	if !cfg.Context.GitContextEnabled {
		return ""
	}

	branch := getGitBranch()

	s.contextCacheMux.RLock()
	if s.gitContextCache != "" && branch == s.gitContextBranch &&
		currentTurn-s.gitContextTurn < cfg.Context.GitContextRefreshTurns {
		defer s.contextCacheMux.RUnlock()
		return s.gitContextCache
	}
	s.contextCacheMux.RUnlock()

	if !isGitRepository() {
		s.contextCacheMux.Lock()
		s.gitContextCache = ""
		s.gitContextTurn = currentTurn
		s.gitContextBranch = branch
		s.contextCacheMux.Unlock()
		return ""
	}

	var gitInfo strings.Builder
	gitInfo.WriteString("\n\nGIT REPOSITORY CONTEXT:")

	if repoName := getGitRepositoryName(); repoName != "" {
		fmt.Fprintf(&gitInfo, "\nRepository: %s", repoName)
	}

	if branch != "" {
		fmt.Fprintf(&gitInfo, "\nCurrent branch: %s", branch)
	}

	if mainBranch := getGitMainBranch(); mainBranch != "" {
		fmt.Fprintf(&gitInfo, "\nMain branch: %s", mainBranch)
	}

	commits := getRecentCommits(5)
	if len(commits) > 0 {
		gitInfo.WriteString("\n\nRecent commits:")
		for _, commit := range commits {
			fmt.Fprintf(&gitInfo, "\n%s", commit)
		}
	}

	result := gitInfo.String()

	s.contextCacheMux.Lock()
	s.gitContextCache = result
	s.gitContextTurn = currentTurn
	s.gitContextBranch = branch
	s.contextCacheMux.Unlock()

	return result
}

// projectTreeMaxLines caps the PROJECT STRUCTURE block in the system prompt.
const projectTreeMaxLines = 100

// buildProjectTreeInfo injects a root-first, depth-prioritized project listing
// into the system prompt so the model reads real paths instead of guessing
// them. It runs the Tree tool in compact format, which in a git repository
// lists tracked plus untracked non-ignored files one directory per line
// (shallow directories first, so the root and top-level layout always fit the
// line budget) and outside a git repo falls back to the ASCII tree. Shares the
// git-context caching scheme, refreshing every GitContextRefreshTurns turns.
func (s *AgentServiceImpl) buildProjectTreeInfo(currentTurn int) string {
	cfg := s.config.GetAgentConfig()
	if !cfg.Context.TreeEnabled {
		return ""
	}

	s.contextCacheMux.RLock()
	if s.treeContextCache != "" && currentTurn-s.treeContextTurn < cfg.Context.GitContextRefreshTurns {
		defer s.contextCacheMux.RUnlock()
		return s.treeContextCache
	}
	s.contextCacheMux.RUnlock()

	result := ""
	if listing := s.compactProjectTree(); listing != "" {
		result = fmt.Sprintf(
			"\n\nPROJECT STRUCTURE (one directory per line, root and shallow directories first; hidden dot-files and dot-directories omitted; \"name.go*\" means name.go plus name_test.go; run the Tree or Grep tools for anything omitted):\n%s",
			listing,
		)
	}

	s.contextCacheMux.Lock()
	s.treeContextCache = result
	s.treeContextTurn = currentTurn
	s.contextCacheMux.Unlock()

	return result
}

// compactProjectTree renders the Tree tool's compact listing for the current
// directory (falling back to the ASCII tree for non-git directories).
func (s *AgentServiceImpl) compactProjectTree() string {
	treeTool := tools.NewTreeTool(s.config)
	execResult, err := treeTool.Execute(context.Background(), map[string]any{
		"path":      ".",
		"format":    "compact",
		"max_files": float64(projectTreeMaxLines),
	})
	if err != nil {
		logger.Debug("failed to build project tree context", "error", err)
		return ""
	}
	if data, ok := execResult.Data.(*agentdomain.TreeToolResult); ok {
		return data.Output
	}
	return ""
}

// gitCommandContext bounds an ad-hoc git shell; the system-prompt build path
// carries no context, so the leaf helpers create their own (same precedent as
// compactProjectTree).
func gitCommandContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), constants.GitCommandTimeout)
}

// isGitRepository checks if the current directory is a git repository
func isGitRepository() bool {
	ctx, cancel := gitCommandContext()
	defer cancel()
	_, err := utils.RunGit(ctx, "", "rev-parse", "--git-dir")
	return err == nil
}

// getGitRepositoryName extracts the repository name from the git remote URL
func getGitRepositoryName() string {
	return project.RemoteName()
}

// getGitBranch returns the current git branch name
func getGitBranch() string {
	ctx, cancel := gitCommandContext()
	defer cancel()
	output, err := utils.RunGit(ctx, "", "branch", "--show-current")
	if err != nil {
		logger.Debug("failed to get current git branch", "error", err)
		return ""
	}

	return strings.TrimSpace(string(output))
}

// getGitMainBranch returns the main branch name (main or master)
func getGitMainBranch() string {
	ctx, cancel := gitCommandContext()
	defer cancel()
	if _, err := utils.RunGit(ctx, "", "rev-parse", "--verify", "main"); err == nil {
		return "main"
	}

	if _, err := utils.RunGit(ctx, "", "rev-parse", "--verify", "master"); err == nil {
		return "master"
	}

	logger.Debug("could not determine main branch (neither 'main' nor 'master' exists)")
	return ""
}

// getRecentCommits returns the last N commit messages
func getRecentCommits(count int) []string {
	ctx, cancel := gitCommandContext()
	defer cancel()
	output, err := utils.RunGit(ctx, "", "log", fmt.Sprintf("-%d", count), "--oneline", "--no-decorate")
	if err != nil {
		logger.Debug("failed to get recent commits", "error", err)
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	return lines
}

// validateRequest validates the agent request
func (s *AgentServiceImpl) validateRequest(req *agentdomain.AgentRequest) error {
	if req == nil {
		return fmt.Errorf("request is nil")
	}
	if req.RequestID == "" {
		return fmt.Errorf("no request ID provided")
	}
	if len(req.Messages) == 0 {
		return fmt.Errorf("no messages provided")
	}
	if req.Model == "" {
		return fmt.Errorf("no model specified")
	}
	return nil
}

// parseProvider parses provider and model name from model string.
// The bare fallback returns "claude" only for legacy un-prefixed inputs.
func (s *AgentServiceImpl) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)

	if len(parts) == 1 {
		if s.config != nil && s.config.GetAgentConfig() != nil {
			return "claude", model, nil
		}
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	return parts[0], parts[1], nil
}

// dispatchHooks runs the actions attached to a hook point: system-reminder
// injection (text action) and command hooks (executable action, #270). Both
// agents flow every loop point through this single seam. The mode key and
// session id are resolved here (from the live chat mode / request context) and
// handed to the shared, allow-list-gated command runner.
func (s *AgentServiceImpl) dispatchHooks(agentCtx *states.AgentContext, hook agentdomain.HookPoint) {
	if hook == agentdomain.HookPreSession && s.memoryBackend != nil {
		_ = s.memoryBackend.SyncIn(agentCtx.Ctx)
	}

	s.injectDueReminders(agentCtx, hook)

	modeKey := agentdomain.AgentModeStandard.AllowedlistKey()
	if s.stateManager != nil {
		modeKey = s.stateManager.GetAgentMode().AllowedlistKey()
	}
	sessionID := ""
	if agentCtx.Ctx != nil {
		sessionID = agentdomain.GetSessionID(agentCtx.Ctx)
	}
	RunCommandHooks(agentCtx.Ctx, s.config, s.hookProvider, modeKey, hook, agentCtx.Turns, sessionID)
}

// waitForBackgroundTasks blocks until in-flight background work (A2A tasks,
// background shells) pushes a result onto the shared message queue or
// quiesces, up to a2a.task.agent_mode_max_wait_seconds. Headless runs call
// this at the completion boundary so a run never exits with orphaned
// background tasks; chat mode never calls it (the UI ticker starts fresh
// turns instead).
func (s *AgentServiceImpl) waitForBackgroundTasks(ctx context.Context) {
	if s.bgRegistry == nil || s.messageQueue == nil || !s.bgRegistry.HasPending() {
		return
	}

	maxWaitSec := 300
	if s.config != nil && s.config.A2A.Task.AgentModeMaxWaitSeconds > 0 {
		maxWaitSec = s.config.A2A.Task.AgentModeMaxWaitSeconds
	}
	deadline := time.NewTimer(time.Duration(maxWaitSec) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	logger.Info("waiting for pending background tasks before completing",
		"running_jobs", s.bgRegistry.CountRunning(),
		"max_wait_seconds", maxWaitSec)

	for {
		select {
		case <-ticker.C:
		case <-deadline.C:
			logger.Warn("timed out waiting for background tasks to complete",
				"running_jobs", s.bgRegistry.CountRunning(),
				"max_wait_seconds", maxWaitSec)
			return
		case <-ctx.Done():
			return
		}
		if !s.messageQueue.IsEmpty() || !s.bgRegistry.HasPending() {
			return
		}
	}
}

// maybeRolloverSession rolls the conversation over into a new session when it
// crosses the compact threshold (SessionRolloverManager gates and performs the
// rollover), then rebuilds the in-flight conversation from the now-compacted
// repository. Called at the top of every headless turn after the first - chat
// rollover is owned by the UI (chat_message_processor), and the first turn is
// handled by the headless command before the run starts.
func (s *AgentServiceImpl) maybeRolloverSession(agentCtx *states.AgentContext, req *agentdomain.AgentRequest) {
	newID, fired := s.rolloverManager.MaybeRollover(agentCtx.Ctx, req.Model, req.GroupKey)
	if !fired {
		return
	}
	logger.Info("rolled over to new session (summary preserved)",
		"previous_session_id", req.RequestID, "new_session_id", newID)
	*agentCtx.Conversation = s.addSystemPrompt(
		conversation.BuildAgentMessagesFromEntries(s.conversationRepo.GetMessages()))
}

// trackStreamOutcome records the just-finished stream's finish reason and
// updates the consecutive no-tool-call strike counter that gates the
// post_stream continuation nudges (on_stalled_todos / on_truncation).
func (s *AgentServiceImpl) trackStreamOutcome(finishReason string, hadToolCalls bool) {
	s.reminderMux.Lock()
	defer s.reminderMux.Unlock()
	s.lastFinishReason = finishReason
	if hadToolCalls {
		s.stalledStrikes = 0
	} else {
		s.stalledStrikes++
	}
}

// incompleteTodoItems filters the session todo list down to items not yet
// completed, for the {todo_list} template of the stalled-todos nudge.
func incompleteTodoItems(todos []agentdomain.TodoItem) []agentdomain.TodoItem {
	var incomplete []agentdomain.TodoItem
	for _, t := range todos {
		if t.Status != "completed" {
			incomplete = append(incomplete, t)
		}
	}
	return incomplete
}

// conversationAwaitsToolResults reports whether the last message is an assistant
// turn carrying tool_calls that have not yet been answered. Injecting a user
// reminder in that state would orphan the tool_calls, so reminder injection is
// skipped until the tool results land.
func conversationAwaitsToolResults(conv []sdk.Message) bool {
	if len(conv) == 0 {
		return false
	}
	last := conv[len(conv)-1]
	return last.Role == sdk.Assistant && last.ToolCalls != nil && len(*last.ToolCalls) > 0
}

// injectDueReminders asks the reminder provider which reminders are due at the
// given hook point, then appends each as a hidden user message, persists it via
// the conversation repo (when wired in), emits a `system_reminder` stream event
// tagged with the reminder name and hook so downstream observers can see the
// nudge, and marks it fired (for the `once` trigger). Multiple reminders due at
// the same hook stack.
//
// Cadence state is session-scoped (s.sessionTurns, s.firedReminders), NOT taken
// from the per-request AgentContext, so `interval` counts cumulative turns
// across the whole chat session and `once` fires once per session. The mutex
// guards the fired-set because the streaming goroutine (pre_session/pre_stream)
// and the event-loop goroutine (the other points) can both reach here.
func (s *AgentServiceImpl) injectDueReminders(agentCtx *states.AgentContext, hook agentdomain.HookPoint) {
	provider := s.reminderProvider
	if provider == nil && s.config != nil {
		provider = s.config.Reminders
	}
	if provider == nil {
		return
	}

	if conversationAwaitsToolResults(*agentCtx.Conversation) {
		return
	}

	s.reminderMux.Lock()
	defer s.reminderMux.Unlock()
	if s.firedReminders == nil {
		s.firedReminders = make(map[string]bool)
	}

	q := agentdomain.ReminderQuery{
		Hook:        hook,
		Turn:        agentCtx.Turns,
		SessionTurn: int(s.sessionTurns.Load()),
		MaxTurns:    agentCtx.MaxTurns,
		Fired:       s.firedReminders,
		ToolFailed:  agentCtx.LastToolFailed,
	}
	if s.stateManager != nil {
		q.TodoCount = len(s.stateManager.GetTodos())
	}
	if hook == agentdomain.HookPostStream && s.stalledStrikes > 0 {
		q.FinishReason = s.lastFinishReason
		q.StalledStrikes = s.stalledStrikes
		if s.lastFinishReason != string(sdk.Length) && s.stateManager != nil {
			q.IncompleteTodos = incompleteTodoItems(s.stateManager.GetTodos())
		}
	}
	if hook == agentdomain.HookPostTool {
		if name, n := s.takeRepeatedFailure(); name != "" {
			q.RepeatedFailures = n
			q.FailedTool = name
		}
	}
	if hook == agentdomain.HookPreStream {
		q.ModeChanged, q.PrevMode, q.Mode = s.modeChangeSinceLastStream()
	}
	InjectDueReminders(provider, q, func(r agentdomain.SystemReminder) {
		if r.AppendToToolResult {
			s.appendToLastToolMessage(agentCtx, r)
		} else {
			s.injectReminderAsUserMessage(agentCtx, r)
		}
	})
}

// InjectDueReminders is the single reminder-injection seam shared by the chat
// (AgentServiceImpl) and headless (AgentSession) loops: it resolves the
// reminders due for q, delivers each via the caller-owned deliver callback
// (the two loops hold different conversation representations), logs it, emits
// the tagged system_reminder stream event, and marks the name in q.Fired.
// Callers own provider resolution, the awaiting-tool-results guard, query
// construction, and locking.
func InjectDueReminders(provider agentdomain.SystemReminderProvider, q agentdomain.ReminderQuery, deliver func(agentdomain.SystemReminder)) {
	for _, r := range provider.RemindersDue(q) {
		deliver(r)
		logger.Debug("system reminder injected",
			"turn", q.Turn,
			"session_turn", q.SessionTurn,
			"hook", string(q.Hook),
			"name", r.Name,
			"reminder_chars", len(r.Text),
		)
		streamevent.EmitDebugMessage("user", r.Text, "system_reminder", map[string]any{
			"turn":         q.Turn,
			"session_turn": q.SessionTurn,
			"hook":         string(q.Hook),
			"name":         r.Name,
		})
		q.Fired[r.Name] = true
	}
}

// appendToLastToolMessage appends reminder text to the last tool-role message
// in the conversation (for on_repeated_failure reminders that require
// tool_call/tool pairing). If no tool message is found, it falls back to
// appending a standalone user message.
func (s *AgentServiceImpl) appendToLastToolMessage(agentCtx *states.AgentContext, r agentdomain.SystemReminder) {
	conv := *agentCtx.Conversation
	for i := len(conv) - 1; i >= 0; i-- {
		if conv[i].Role == sdk.Tool {
			if txt, err := conv[i].Content.AsMessageContent0(); err == nil {
				conv[i].Content = sdk.NewMessageContent(txt + "\n\n" + r.Text)
			} else {
				conv[i].Content = sdk.NewMessageContent(r.Text)
			}
			return
		}
	}
	msg := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(r.Text)}
	*agentCtx.Conversation = append(*agentCtx.Conversation, msg)
}

// injectReminderAsUserMessage appends a reminder as a hidden user message,
// persisting it when a conversation repo is wired in.
func (s *AgentServiceImpl) injectReminderAsUserMessage(agentCtx *states.AgentContext, r agentdomain.SystemReminder) {
	msg := sdk.Message{Role: sdk.User, Content: sdk.NewMessageContent(r.Text)}
	*agentCtx.Conversation = append(*agentCtx.Conversation, msg)

	if s.conversationRepo != nil {
		entry := convdomain.ConversationEntry{Message: msg, Time: time.Now(), Hidden: true}
		if err := s.conversationRepo.AddMessage(entry); err != nil {
			logger.Error("failed to store system reminder message", "error", err)
		}
	}
}

// modeChangeSinceLastStream reads the live agent mode and compares it against
// the previous streaming turn's mode, advancing the stored baseline. The first
// call seeds the baseline without reporting a change; a nil stateManager (the
// headless path has no mid-session mode) never reports a change. The result
// drives the on_mode_change reminder trigger via ReminderQuery.
func (s *AgentServiceImpl) modeChangeSinceLastStream() (changed bool, prev, cur agentdomain.AgentMode) {
	if s.stateManager == nil {
		return false, agentdomain.AgentModeStandard, agentdomain.AgentModeStandard
	}
	cur = s.stateManager.GetAgentMode()

	s.modeMux.Lock()
	defer s.modeMux.Unlock()
	if !s.modeInitialized {
		s.modeInitialized = true
		s.lastStreamedMode = cur
		return false, cur, cur
	}
	prev = s.lastStreamedMode
	s.lastStreamedMode = cur
	return prev != cur, prev, cur
}

// isCompleteJSON checks if a string is a complete, valid JSON
func isCompleteJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}

	var js any
	return json.Unmarshal([]byte(s), &js) == nil
}

// getTruncationRecoveryGuidance returns tool-specific guidance when a tool call is truncated
func getTruncationRecoveryGuidance(toolName string) string {
	switch toolName {
	case "Write":
		return "YOU MUST use a different approach: " +
			"1. First create an EMPTY or MINIMAL file using Write with just a skeleton/placeholder. " +
			"2. Then use the Edit tool to add content in small chunks (20-30 lines per Edit call). " +
			"3. Repeat Edit calls until the file is complete. " +
			"DO NOT attempt to Write the full content again - it will fail the same way."
	case "Edit":
		return "YOUR EDIT WAS TOO LARGE. YOU MUST: " +
			"1. Break your edit into SMALLER chunks (10-20 lines maximum per Edit call). " +
			"2. Use a shorter, more precise old_string to match. " +
			"3. Make multiple smaller Edit calls instead of one large edit. " +
			"DO NOT retry with the same large edit - it will fail again."
	case "Bash":
		return "Your command output or arguments were too large. " +
			"Try breaking the command into smaller parts or redirecting output to a file."
	default:
		return "The tool arguments were too large. " +
			"Try breaking your request into smaller, incremental operations."
	}
}
