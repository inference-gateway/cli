package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
)

// accumulateToolCalls processes multiple tool call deltas and stores them in the agent's toolCallsMap
func (s *AgentServiceImpl) accumulateToolCalls(deltas []sdk.ChatCompletionMessageToolCallChunk) {
	s.toolCallsMux.Lock()
	defer s.toolCallsMux.Unlock()

	for _, delta := range deltas {
		key := fmt.Sprintf("%d", delta.Index)

		if s.toolCallsMap[key] == nil {
			s.toolCallsMap[key] = &sdk.ChatCompletionMessageToolCall{
				Id:   delta.ID,
				Type: sdk.Function,
				Function: sdk.ChatCompletionMessageToolCallFunction{
					Name:      "",
					Arguments: "",
				},
			}
		}

		toolCall := s.toolCallsMap[key]
		if delta.ID != "" {
			toolCall.Id = delta.ID
		}
		if delta.Function.Name != "" && toolCall.Function.Name == "" {
			toolCall.Function.Name = delta.Function.Name
		}
		if delta.Function.Arguments != "" {
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
	sort.Ints(indices)

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

// addSystemPrompt adds system prompt with dynamic sandbox info and returns messages
func (s *AgentServiceImpl) addSystemPrompt(messages []sdk.Message) []sdk.Message {
	var systemMessages []sdk.Message

	baseSystemPrompt := s.getSystemPromptForMode()
	if baseSystemPrompt != "" {
		currentTime := time.Now().Format("Monday, January 2, 2006 at 3:04 PM MST")

		currentTurn := len(messages) / 2

		sandboxInfo := s.buildSandboxInfo()
		a2aAgentInfo := s.buildA2AAgentInfo()
		osInfo := s.buildOSInfo()
		workingDirInfo := s.buildWorkingDirectoryInfo()
		gitContextInfo := s.buildGitContextInfo(currentTurn)

		systemPromptWithInfo := fmt.Sprintf("%s\n\n%s%s%s%s%s\n\nCurrent date and time: %s",
			baseSystemPrompt,
			sandboxInfo,
			a2aAgentInfo,
			osInfo,
			workingDirInfo,
			gitContextInfo,
			currentTime)

		systemMessages = append(systemMessages, sdk.Message{
			Role:    sdk.System,
			Content: sdk.NewMessageContent(systemPromptWithInfo),
		})
	}

	if len(systemMessages) > 0 {
		messages = append(systemMessages, messages...)
	}
	return messages
}

// getSystemPromptForMode returns the appropriate system prompt based on current agent mode
func (s *AgentServiceImpl) getSystemPromptForMode() string {
	agentConfig := s.config.GetAgentConfig()

	if s.stateManager == nil {
		return agentConfig.SystemPrompt
	}

	mode := s.stateManager.GetAgentMode()
	switch mode {
	case domain.AgentModePlan:
		if agentConfig.SystemPromptPlan != "" {
			return agentConfig.SystemPromptPlan
		}
		return agentConfig.SystemPrompt

	case domain.AgentModeAutoAccept:
		return agentConfig.SystemPrompt

	case domain.AgentModeStandard:
		return agentConfig.SystemPrompt

	default:
		return agentConfig.SystemPrompt
	}
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
			sandboxInfo.WriteString(fmt.Sprintf("- %s\n", dir))
		}
		sandboxInfo.WriteString("\n")
	}

	if len(protectedPaths) > 0 {
		sandboxInfo.WriteString("You MUST NOT attempt to access these protected paths:\n")
		for _, path := range protectedPaths {
			sandboxInfo.WriteString(fmt.Sprintf("- %s\n", path))
		}
	}

	return sandboxInfo.String()
}

// buildOSInfo creates dynamic OS information for the system prompt
func (s *AgentServiceImpl) buildOSInfo() string {
	osInfo := fmt.Sprintf("\n\nOPERATING SYSTEM: %s", runtime.GOOS)

	switch runtime.GOOS {
	case "darwin":
		osInfo += "\n- Use 'cmd' modifier for keyboard shortcuts (e.g., 'cmd+c' for copy)"
		osInfo += "\n- Use 'open -a AppName' to launch applications (e.g., 'open -a Firefox')"
		osInfo += "\n- Use 'open file.txt' to open files with default app"
		osInfo += "\n- IMPORTANT: When opening URLs in browsers, ALWAYS open in a NEW WINDOW, not a new tab"
		osInfo += "\n  Example: 'open -n -a \"Google Chrome\" --args --new-window https://example.com'"
		osInfo += "\n  Or for Safari: 'open -n -a Safari https://example.com'"
		osInfo += "\n  This allows proper focus management between windows"
	case "linux":
		osInfo += "\n- Use 'ctrl' modifier for keyboard shortcuts (e.g., 'ctrl+c' for copy)"
		osInfo += "\n- Use command name directly or 'xdg-open' to launch applications"
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
		logger.Debug("Failed to get working directory: %v", err)
		return ""
	}

	return fmt.Sprintf("\n\nWORKING DIRECTORY: %s", workingDir)
}

// buildGitContextInfo creates dynamic git repository information for the system prompt
func (s *AgentServiceImpl) buildGitContextInfo(currentTurn int) string {
	cfg := s.config.GetAgentConfig()
	if !cfg.Context.GitContextEnabled {
		return ""
	}

	s.contextCacheMux.RLock()
	if s.gitContextCache != "" && currentTurn-s.gitContextTurn < cfg.Context.GitContextRefreshTurns {
		defer s.contextCacheMux.RUnlock()
		return s.gitContextCache
	}
	s.contextCacheMux.RUnlock()

	if !isGitRepository() {
		s.contextCacheMux.Lock()
		s.gitContextCache = ""
		s.gitContextTurn = currentTurn
		s.contextCacheMux.Unlock()
		return ""
	}

	var gitInfo strings.Builder
	gitInfo.WriteString("\n\nGIT REPOSITORY CONTEXT:")

	if repoName := getGitRepositoryName(); repoName != "" {
		gitInfo.WriteString(fmt.Sprintf("\nRepository: %s", repoName))
	}

	if branch := getGitBranch(); branch != "" {
		gitInfo.WriteString(fmt.Sprintf("\nCurrent branch: %s", branch))
	}

	if mainBranch := getGitMainBranch(); mainBranch != "" {
		gitInfo.WriteString(fmt.Sprintf("\nMain branch: %s", mainBranch))
	}

	commits := getRecentCommits(5)
	if len(commits) > 0 {
		gitInfo.WriteString("\n\nRecent commits:")
		for _, commit := range commits {
			gitInfo.WriteString(fmt.Sprintf("\n%s", commit))
		}
	}

	result := gitInfo.String()

	s.contextCacheMux.Lock()
	s.gitContextCache = result
	s.gitContextTurn = currentTurn
	s.contextCacheMux.Unlock()

	return result
}

// isGitRepository checks if the current directory is a git repository
func isGitRepository() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// getGitRepositoryName extracts the repository name from the git remote URL
func getGitRepositoryName() string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		logger.Debug("Failed to get git remote URL: %v", err)
		return ""
	}

	remoteURL := strings.TrimSpace(string(output))

	httpsPattern := regexp.MustCompile(`^https?://[^/]+/([^/]+/[^/]+?)(?:\.git)?$`)
	if matches := httpsPattern.FindStringSubmatch(remoteURL); len(matches) > 1 {
		return matches[1]
	}

	sshPattern := regexp.MustCompile(`^git@[^:]+:([^/]+/[^/]+?)(?:\.git)?$`)
	if matches := sshPattern.FindStringSubmatch(remoteURL); len(matches) > 1 {
		return matches[1]
	}

	logger.Debug("Could not parse git repository name from URL: %s", remoteURL)
	return ""
}

// getGitBranch returns the current git branch name
func getGitBranch() string {
	cmd := exec.Command("git", "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		logger.Debug("Failed to get current git branch: %v", err)
		return ""
	}

	return strings.TrimSpace(string(output))
}

// getGitMainBranch returns the main branch name (main or master)
func getGitMainBranch() string {
	cmd := exec.Command("git", "rev-parse", "--verify", "main")
	if err := cmd.Run(); err == nil {
		return "main"
	}

	cmd = exec.Command("git", "rev-parse", "--verify", "master")
	if err := cmd.Run(); err == nil {
		return "master"
	}

	logger.Debug("Could not determine main branch (neither 'main' nor 'master' exists)")
	return ""
}

// getRecentCommits returns the last N commit messages
func getRecentCommits(count int) []string {
	cmd := exec.Command("git", "log", fmt.Sprintf("-%d", count), "--oneline", "--no-decorate")
	output, err := cmd.Output()
	if err != nil {
		logger.Debug("Failed to get recent commits: %v", err)
		return nil
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	return lines
}

// validateRequest validates the agent request
func (s *AgentServiceImpl) validateRequest(req *domain.AgentRequest) error {
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

// parseProvider parses provider and model name from model string
func (s *AgentServiceImpl) parseProvider(model string) (string, string, error) {
	parts := strings.SplitN(model, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid model format, expected 'provider/model'")
	}

	return parts[0], parts[1], nil
}

// shouldInjectSystemReminder checks if a system reminder should be injected
func (s *AgentServiceImpl) shouldInjectSystemReminder(turns int) bool {
	cfg := s.config.GetAgentConfig()

	if !cfg.SystemReminders.Enabled {
		return false
	}

	interval := cfg.SystemReminders.Interval
	if interval <= 0 {
		interval = 4
	}

	return turns > 0 && turns%interval == 0
}

// getSystemReminderMessage returns the system reminder message to inject
func (s *AgentServiceImpl) getSystemReminderMessage() sdk.Message {
	cfg := s.config.GetAgentConfig()

	reminderText := cfg.SystemReminders.ReminderText
	if reminderText == "" {
		reminderText = `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`
	}

	return sdk.Message{
		Role:    sdk.User,
		Content: sdk.NewMessageContent(reminderText),
	}
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

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
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
