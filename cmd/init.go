package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	uuid "github.com/google/uuid"
	config "github.com/inference-gateway/cli/config"
	container "github.com/inference-gateway/cli/internal/container"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	icons "github.com/inference-gateway/cli/internal/ui/styles/icons"
	sdk "github.com/inference-gateway/sdk"
	cobra "github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new project with Inference Gateway CLI",
	Long: `Initialize a new project directory with Inference Gateway CLI configuration.
This creates the .infer directory with configuration file and additional setup files like .gitignore.

By default, generates a basic AGENTS.md file. Use --model <provider>/<model> to generate an
AI-analyzed project-specific AGENTS.md file.

This is the recommended command to start working with Inference Gateway CLI in a new project.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return initializeProject(cmd)
	},
}

func init() {
	initCmd.Flags().Bool("overwrite", false, "Overwrite existing files if they already exist")
	initCmd.Flags().Bool("userspace", false, "Initialize configuration in user home directory (~/.infer/)")
	initCmd.Flags().Bool("skip-agents-md", false, "Skip generating AGENTS.md file during initialization")
	initCmd.Flags().String("model", "", "LLM model to use for generating AGENTS.md file (if not specified, generates default AGENTS.md)")
	rootCmd.AddCommand(initCmd)
}

func initializeProject(cmd *cobra.Command) error {
	overwrite, _ := cmd.Flags().GetBool("overwrite")
	userspace, _ := cmd.Flags().GetBool("userspace")
	skipAgentsMD, _ := cmd.Flags().GetBool("skip-agents-md")
	model, _ := cmd.Flags().GetString("model")

	var configPath, gitignorePath, agentsMDPath string

	if userspace {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get user home directory: %w", err)
		}
		configPath = filepath.Join(homeDir, config.ConfigDirName, config.ConfigFileName)
		gitignorePath = filepath.Join(homeDir, config.ConfigDirName, config.GitignoreFileName)
		agentsMDPath = filepath.Join(homeDir, config.ConfigDirName, "AGENTS.md")
	} else {
		configPath = config.DefaultConfigPath
		gitignorePath = filepath.Join(config.ConfigDirName, config.GitignoreFileName)
		agentsMDPath = "AGENTS.md"
	}

	if !overwrite {
		if err := validateFilesNotExist(configPath, gitignorePath, agentsMDPath, skipAgentsMD); err != nil {
			return err
		}
	}

	if err := writeConfigAsYAMLWithIndent(configPath, 2); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	gitignoreContent := `# Ignore log files and history files
logs/*.log
history
chat_export_*
conversations.db
`

	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("failed to create .gitignore file: %w", err)
	}

	if !skipAgentsMD {
		if err := generateAgentsMD(agentsMDPath, userspace, model); err != nil {
			return fmt.Errorf("failed to create AGENTS.md file: %w", err)
		}
	}

	var scopeDesc string
	if userspace {
		scopeDesc = "userspace"
	} else {
		scopeDesc = "project"
	}

	fmt.Printf("%s Successfully initialized Inference Gateway CLI %s configuration\n", icons.CheckMarkStyle.Render(icons.CheckMark), scopeDesc)
	fmt.Printf("   Created: %s\n", configPath)
	fmt.Printf("   Created: %s\n", gitignorePath)
	if !skipAgentsMD {
		fmt.Printf("   Created: %s\n", agentsMDPath)
		if model == "" && !userspace {
			fmt.Printf("   ⚠️  Generated default AGENTS.md (use --model <provider>/<model> for AI-generated content)\n")
		}
	}
	fmt.Println("")
	if userspace {
		fmt.Println("This userspace configuration will be used as a fallback for all projects.")
		fmt.Println("Project-level configurations will take precedence when present.")
		fmt.Println("")
	}
	fmt.Println("You can now customize the configuration:")
	fmt.Println("  • Set default model: infer config agent set-model <model-name>")
	fmt.Println("  • Configure tools: infer config tools --help")
	fmt.Println("  • Start chatting: infer chat")

	return nil
}

// writeConfigAsYAMLWithIndent writes the default configuration to a YAML file with specified indentation
func writeConfigAsYAMLWithIndent(filename string, indent int) error {
	defaultConfig := config.DefaultConfig()

	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	var buf bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&buf)
	yamlEncoder.SetIndent(indent)

	if err := yamlEncoder.Encode(defaultConfig); err != nil {
		return fmt.Errorf("failed to marshal config to YAML: %w", err)
	}

	if err := yamlEncoder.Close(); err != nil {
		return fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	return os.WriteFile(filename, buf.Bytes(), 0644)
}

// generateAgentsMD creates an AGENTS.md file based on project analysis
func generateAgentsMD(agentsMDPath string, userspace bool, model string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	content, err := analyzeProjectForAgents(wd, userspace, model)
	if err != nil {
		return fmt.Errorf("failed to analyze project: %w", err)
	}

	if err := os.WriteFile(agentsMDPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write AGENTS.md: %w", err)
	}

	return nil
}

// projectResearchSystemPrompt returns the system prompt for project research
func projectResearchSystemPrompt() string {
	return `You are a specialized project analysis agent. Your task is to research and understand a software project to create comprehensive documentation for other AI agents working on this project.

ANALYSIS OBJECTIVES:
- Understand the project's architecture, structure, and technologies
- Identify development workflows, build processes, and testing approaches
- Discover project conventions, coding standards, and patterns
- Extract key configuration files, dependencies, and environment setup
- Document important commands, scripts, and automation tools

OUTPUT FORMAT:
Generate an AGENTS.md file following this structure:

# AGENTS.md

## Project Overview
Brief description of what this project does and its main technologies.

## Architecture & Structure
Key directories, modules, and architectural patterns used.

## Development Environment
- Setup instructions and dependencies
- Required tools and versions
- Environment variables and configuration

## Development Workflow
- Build commands and processes
- Testing procedures and commands
- Code quality tools (linting, formatting)
- Git workflow and branching strategy

## Key Commands
Essential commands developers use regularly:
- Build: ` + "`command`" + `
- Test: ` + "`command`" + `
- Lint: ` + "`command`" + `
- Run: ` + "`command`" + `

## Testing Instructions
- How to run tests
- Test organization and patterns
- Coverage requirements

## Deployment & Release
- Deployment processes
- Release procedures
- CI/CD pipeline information

## Project Conventions
- Coding standards and style guides
- Naming conventions
- File organization patterns
- Commit message formats

## Important Files & Configurations
Key files that agents should be aware of and their purposes.

RESEARCH APPROACH:
1. Start by reading package.json, go.mod, Cargo.toml, or similar dependency files
2. Look for README files, documentation, and setup guides
3. Examine build scripts, Makefiles, or task runners (package.json scripts, Taskfile.yml, etc.)
4. Check for configuration files (.gitignore, .env examples, config files)
5. Identify testing frameworks and CI/CD configurations
6. Look for code quality tools configurations
7. Examine directory structure and common patterns

IMPORTANT GUIDELINES:
- Be concise but comprehensive - agents need actionable information
- Focus on practical development tasks, not theoretical concepts
- Include specific commands and file paths when relevant
- Prioritize information that helps agents work effectively on the project
- Use clear, direct language without unnecessary elaboration
- If information is missing or unclear, acknowledge it rather than guessing

TOOL USAGE:
- Use available tools to explore the project (Tree, Read, Grep, etc.)
- When you have gathered enough information, use the Write tool to create the AGENTS.md file
- Write the file content directly without code fences or API calls
- The Write tool expects: Write(file_path="/path/to/file", content="file content here")

Your analysis should help other agents quickly understand how to work with this project effectively.`
}

// analyzeProjectForAgents analyzes the current project and generates AGENTS.md content
func analyzeProjectForAgents(projectDir string, userspace bool, model string) (string, error) {
	if userspace {
		return getDefaultAgentsMDContent(), nil
	}

	if model == "" {
		return getDefaultAgentsMDContent(), nil
	}

	if V == nil {
		return getDefaultAgentsMDContent(), nil
	}

	cfg, err := getConfigFromViper()
	if err != nil {
		return getDefaultAgentsMDContent(), nil
	}

	if cfg == nil {
		return getDefaultAgentsMDContent(), nil
	}

	cfgCopy := *cfg
	cfgCopy.Agent.SystemPrompt = projectResearchSystemPrompt()

	services := container.NewServiceContainer(&cfgCopy, V)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfgCopy.Gateway.Timeout)*time.Second)
	defer cancel()

	models, err := services.GetModelService().ListModels(ctx)
	if err != nil {
		return getDefaultAgentsMDContent(), nil
	}

	if len(models) == 0 {
		return getDefaultAgentsMDContent(), nil
	}

	if !isModelAvailable(models, model) {
		return getDefaultAgentsMDContent(), nil
	}

	agentService := services.GetAgentService()
	toolService := services.GetToolService()
	if agentService == nil || toolService == nil {
		return getDefaultAgentsMDContent(), nil
	}

	session := &ProjectAnalysisSession{
		agentService:   agentService,
		toolService:    toolService,
		model:          model,
		config:         &cfgCopy,
		conversation:   []InitConversationMessage{},
		maxTurns:       cfgCopy.Agent.MaxTurns,
		startTime:      time.Now(),
		timeoutSeconds: 30,
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s\n", `{"content":"Initializing project analysis session...","timestamp":"`+time.Now().Format("15:04:05")+`","elapsed":"0.0s","tokens":{"input":0,"output":0,"total":0}}`)
	_ = os.Stdout.Sync()

	result, err := session.analyze(fmt.Sprintf("Please analyze the project in directory '%s' and generate a comprehensive AGENTS.md file. Use your available tools to examine the project structure, configuration files, documentation, build systems, and development workflow. Focus on creating actionable documentation that will help other AI agents understand how to work effectively with this project.", projectDir))
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "%s\n", `{"role":"error","content":"Analysis failed, using default content","timestamp":"`+time.Now().Format("15:04:05")+`","elapsed":"0.0s","tokens":{"input":0,"output":0,"total":0}}`)
		_ = os.Stdout.Sync()
		return getDefaultAgentsMDContent(), nil
	}

	return result, nil
}

// checkFileExists checks if a file exists and returns an error if it does
func checkFileExists(path, description string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s %s already exists (use --overwrite to replace)", description, path)
	}
	return nil
}

// validateFilesNotExist validates that required files do not exist
func validateFilesNotExist(configPath, gitignorePath, agentsMDPath string, skipAgentsMD bool) error {
	if err := checkFileExists(configPath, "configuration file"); err != nil {
		return err
	}
	if err := checkFileExists(gitignorePath, ".gitignore file"); err != nil {
		return err
	}
	if !skipAgentsMD {
		return checkFileExists(agentsMDPath, "AGENTS.md file")
	}
	return nil
}

// getProjectAnalysisModel returns the model to use for project analysis
func getProjectAnalysisModel() string {
	if model := os.Getenv("INFER_AGENT_MODEL"); model != "" {
		return model
	}
	return "anthropic/claude-3-haiku"
}

// getDefaultAgentsMDContent returns default AGENTS.md content when analysis fails
func getDefaultAgentsMDContent() string {
	return `# AGENTS.md

## Project Overview
This project uses the Inference Gateway CLI for AI-powered development workflows.

## Development Environment
- Ensure you have the Inference Gateway CLI configured
- Check project-specific requirements in README.md or documentation
- Configure your development environment according to project standards

## Development Workflow
- Initialize the project with proper configuration
- Follow established coding patterns and conventions
- Run tests before committing changes
- Use appropriate build and deployment processes

## Key Commands
Check the following files for project-specific commands:
- package.json scripts
- Makefile targets
- Taskfile.yml tasks
- README.md instructions

## Testing Instructions
- Locate and examine test files in the project
- Run test suites using project-specific test runners
- Ensure all tests pass before submitting changes

## Project Conventions
- Follow the coding style established in the codebase
- Respect existing architecture patterns
- Use consistent naming conventions
- Follow project-specific commit message formats

## Important Files & Configurations
- Configuration files in project root
- Environment variable templates (.env.example)
- Build and deployment scripts
- Documentation files

*This AGENTS.md was generated automatically. For more specific project information, examine the codebase directly or refer to project documentation.*
`
}

// InitConversationMessage represents a message in the analysis conversation
type InitConversationMessage struct {
	Role       string                               `json:"role"`
	Content    string                               `json:"content"`
	ToolCalls  *[]sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	Tools      []string                             `json:"tools,omitempty"`
	ToolCallID string                               `json:"tool_call_id,omitempty"`
	TokenUsage *sdk.CompletionUsage                 `json:"token_usage,omitempty"`
	Timestamp  time.Time                            `json:"timestamp"`
	RequestID  string                               `json:"request_id,omitempty"`
	Internal   bool                                 `json:"-"`
}

// ProjectAnalysisSession manages the iterative analysis session for generating AGENTS.md
type ProjectAnalysisSession struct {
	agentService        domain.AgentService
	toolService         domain.ToolService
	model               string
	conversation        []InitConversationMessage
	maxTurns            int
	completedTurns      int
	config              *config.Config
	startTime           time.Time
	timeoutSeconds      int
	totalInputTokens    int64
	totalOutputTokens   int64
	timeoutMessageCount int
}

func (s *ProjectAnalysisSession) analyze(taskDescription string) (string, error) {
	logger.Info("starting project analysis session",
		"model", s.model,
		"timeout_seconds", s.timeoutSeconds,
		"max_turns", s.maxTurns)
	s.outputProgress("session_start", "Starting project analysis...", "")

	s.addMessage(InitConversationMessage{
		Role:      "user",
		Content:   taskDescription,
		Timestamp: time.Now(),
	})

	consecutiveNoToolCalls := 0
	var lastAssistantResponse string
	timeoutReached := false

	for s.completedTurns < s.maxTurns {
		if err := s.executeTurn(); err != nil {
			return getDefaultAgentsMDContent(), err
		}

		s.completedTurns++

		if s.hasTimedOut() {
			if !timeoutReached {
				elapsed := time.Since(s.startTime).Seconds()
				logger.Debug("initial timeout reached, injecting Write tool instruction",
					"elapsed_seconds", elapsed,
					"timeout_seconds", s.timeoutSeconds)
				s.outputProgress("timeout", "Timeout reached, requesting final output...", "")
				timeoutReached = true
				s.timeoutMessageCount = 1

				timeoutMsg := InitConversationMessage{
					Role:      "user",
					Content:   "TIME LIMIT REACHED. You must now complete the task immediately. Use the Write tool to create the AGENTS.md file with all the information you have gathered during your analysis. IMPORTANT: Write the content directly without any markdown code fences (```markdown) at the beginning or end - just write the raw markdown content. If you have been tracking todos, include any remaining incomplete tasks in a 'Future Work' or 'TODO' section so future agents know what still needs to be explored. After writing the file, do NOT make any more tool calls. Write a comprehensive AGENTS.md file based on your exploration so far.",
					Timestamp: time.Now(),
					Internal:  false,
				}
				s.addMessage(timeoutMsg)
			} else if s.completedTurns%2 == 0 && s.timeoutMessageCount < 3 {
				s.timeoutMessageCount++
				elapsed := time.Since(s.startTime).Seconds()
				logger.Debug("re-injecting timeout message",
					"attempt", s.timeoutMessageCount,
					"elapsed_seconds", elapsed,
					"completed_turns", s.completedTurns)
				s.outputProgress("timeout_repeat", fmt.Sprintf("Timeout reminder %d - use Write tool now!", s.timeoutMessageCount), "")

				timeoutMsg := InitConversationMessage{
					Role:      "user",
					Content:   "TIME LIMIT REACHED. You must now complete the task immediately. Use the Write tool to create the AGENTS.md file with all the information you have gathered during your analysis. IMPORTANT: Write the content directly without any markdown code fences (```markdown) at the beginning or end - just write the raw markdown content. If you have been tracking todos, include any remaining incomplete tasks in a 'Future Work' or 'TODO' section so future agents know what still needs to be explored. After writing the file, do NOT make any more tool calls. Write a comprehensive AGENTS.md file based on your exploration so far.",
					Timestamp: time.Now(),
					Internal:  false,
				}
				s.addMessage(timeoutMsg)
			}
		}

		if timeoutReached && s.timeoutMessageCount >= 3 && s.completedTurns > 6 {
			elapsed := time.Since(s.startTime).Seconds()
			logger.Debug("forcing session end due to ignored timeout messages",
				"timeout_messages_sent", s.timeoutMessageCount,
				"completed_turns", s.completedTurns,
				"elapsed_seconds", elapsed)
			s.outputProgress("forced_exit", "Agent ignored timeout instructions, ending session", "")
			break
		}

		if s.lastResponseHadNoToolCalls() {
			consecutiveNoToolCalls++

			if consecutiveNoToolCalls >= 2 || timeoutReached {
				break
			}

			logger.Debug("adding fallback completion message",
				"reason", "no tool calls detected",
				"consecutive_no_tool_calls", consecutiveNoToolCalls,
				"timeout_reached", timeoutReached)
			verifyMsg := InitConversationMessage{
				Role:      "user",
				Content:   "Please provide the final AGENTS.md content based on your analysis. The content should be the complete markdown file ready for use.",
				Timestamp: time.Now(),
				Internal:  true,
			}
			s.addMessage(verifyMsg)
		} else {
			consecutiveNoToolCalls = 0
		}

		lastAssistantResponse = s.getLastAssistantContent()
	}

	if lastAssistantResponse == "" {
		logger.Warn("no assistant response generated, using default AGENTS.md content",
			"completed_turns", s.completedTurns,
			"elapsed_seconds", time.Since(s.startTime).Seconds())
		s.outputProgress("complete", "No response generated, using default content", "")
		return getDefaultAgentsMDContent(), nil
	}

	logger.Info("project analysis session completed successfully",
		"completed_turns", s.completedTurns,
		"elapsed_seconds", time.Since(s.startTime).Seconds(),
		"total_input_tokens", s.totalInputTokens,
		"total_output_tokens", s.totalOutputTokens,
		"timeout_messages_sent", s.timeoutMessageCount)
	s.outputProgress("complete", "Analysis complete, AGENTS.md generated", "")
	return lastAssistantResponse, nil
}

func (s *ProjectAnalysisSession) executeTurn() error {
	ctx := context.Background()
	requestID := uuid.New().String()

	messages := s.buildSDKMessages()

	req := &domain.AgentRequest{
		RequestID: requestID,
		Model:     s.model,
		Messages:  messages,
	}

	response, err := s.agentService.Run(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return s.processSyncResponse(response, requestID)
}

func (s *ProjectAnalysisSession) buildSDKMessages() []sdk.Message {
	var messages []sdk.Message

	for _, msg := range s.conversation {
		if msg.Internal {
			continue
		}

		var role sdk.MessageRole
		switch msg.Role {
		case "user":
			role = sdk.User
		case "assistant":
			role = sdk.Assistant
		case "tool":
			role = sdk.Tool
		case "system":
			role = sdk.System
		default:
			role = sdk.User
		}

		sdkMsg := sdk.Message{
			Role:    role,
			Content: msg.Content,
		}

		if msg.ToolCalls != nil && len(*msg.ToolCalls) > 0 {
			sdkMsg.ToolCalls = msg.ToolCalls
		}

		if msg.ToolCallID != "" {
			sdkMsg.ToolCallId = &msg.ToolCallID
		}

		messages = append(messages, sdkMsg)
	}

	return messages
}

func (s *ProjectAnalysisSession) processSyncResponse(response *domain.ChatSyncResponse, requestID string) error {
	if response.Usage != nil {
		s.totalOutputTokens += response.Usage.CompletionTokens
		s.totalInputTokens = response.Usage.PromptTokens
	}

	if response.Content != "" {
		assistantMsg := InitConversationMessage{
			Role:       "assistant",
			Content:    response.Content,
			TokenUsage: response.Usage,
			Timestamp:  time.Now(),
			RequestID:  requestID,
		}
		s.addMessage(assistantMsg)
		s.outputAssistantProgress(response.Content)
	}

	for i, toolCall := range response.ToolCalls {
		if toolCall.Id == "" {
			toolCall.Id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
		}

		toolCallMsg := InitConversationMessage{
			Role:      "assistant",
			Content:   "",
			ToolCalls: &[]sdk.ChatCompletionMessageToolCall{toolCall},
			Timestamp: time.Now(),
			RequestID: requestID,
		}
		s.addMessage(toolCallMsg)

		var argsMap map[string]any
		_ = json.Unmarshal([]byte(toolCall.Function.Arguments), &argsMap)
		s.outputToolCallProgress(toolCall.Function.Name, argsMap)

		result, err := s.executeToolCall(toolCall.Function.Name, toolCall.Function.Arguments)
		if err != nil {
			s.outputProgress("tool_error", fmt.Sprintf("Tool %s failed: %s", toolCall.Function.Name, err.Error()), "")
			continue
		}

		s.outputToolResultProgress(toolCall.Function.Name, result)

		toolResultMsg := InitConversationMessage{
			Role:       "tool",
			Content:    s.formatToolResult(result),
			ToolCallID: toolCall.Id,
			Timestamp:  time.Now(),
		}
		s.addMessage(toolResultMsg)
	}

	return nil
}

func (s *ProjectAnalysisSession) executeToolCall(toolName, args string) (*domain.ToolExecutionResult, error) {
	var argsMap map[string]any
	if err := json.Unmarshal([]byte(args), &argsMap); err != nil {
		return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
	}

	if err := s.toolService.ValidateTool(toolName, argsMap); err != nil {
		return nil, fmt.Errorf("tool validation failed: %w", err)
	}

	ctx := context.Background()
	toolCall := sdk.ChatCompletionMessageToolCallFunction{
		Name:      toolName,
		Arguments: args,
	}
	return s.toolService.ExecuteTool(ctx, toolCall)
}

func (s *ProjectAnalysisSession) formatToolResult(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if !result.Success {
		return fmt.Sprintf("Tool execution failed: %s", result.Error)
	}

	resultBytes, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("Result of tool call: %v", result.Data)
	}

	return fmt.Sprintf("Result of tool call: %s", string(resultBytes))
}

func (s *ProjectAnalysisSession) addMessage(msg InitConversationMessage) {
	s.conversation = append(s.conversation, msg)
}

func (s *ProjectAnalysisSession) lastResponseHadNoToolCalls() bool {
	if len(s.conversation) < 2 {
		return false
	}

	for i := len(s.conversation) - 1; i >= 0; i-- {
		msg := s.conversation[i]
		if msg.Role == "assistant" {
			return msg.ToolCalls == nil || len(*msg.ToolCalls) == 0
		}
	}

	return false
}

func (s *ProjectAnalysisSession) getLastAssistantContent() string {
	for i := len(s.conversation) - 1; i >= 0; i-- {
		msg := s.conversation[i]
		if msg.Role == "assistant" && msg.Content != "" {
			return msg.Content
		}
	}
	return ""
}

func (s *ProjectAnalysisSession) hasTimedOut() bool {
	return time.Since(s.startTime) > time.Duration(s.timeoutSeconds)*time.Second
}

func (s *ProjectAnalysisSession) outputAssistantProgress(content string) {
	s.outputProgress("assistant", s.truncateContent(content, 150), "")
}

func (s *ProjectAnalysisSession) outputToolCallProgress(toolName string, args map[string]any) {
	argsStr := ""
	if len(args) > 0 {
		argBytes, _ := json.Marshal(args)
		argsStr = s.truncateContent(string(argBytes), 100)
	}
	s.outputProgress("tool_call", fmt.Sprintf("Executing %s", toolName), argsStr)
}

func (s *ProjectAnalysisSession) outputToolResultProgress(toolName string, result *domain.ToolExecutionResult) {
	status := "failed"
	if result != nil && result.Success {
		status = "success"
	}
	s.outputProgress("tool_result", fmt.Sprintf("Tool %s completed: %s", toolName, status), "")
}

func (s *ProjectAnalysisSession) outputProgress(role, content, extra string) {
	progressData := map[string]any{
		"role":      role,
		"content":   content,
		"timestamp": time.Now().Format("15:04:05"),
		"elapsed":   fmt.Sprintf("%.1fs", time.Since(s.startTime).Seconds()),
		"tokens": map[string]any{
			"input":  s.totalInputTokens,
			"output": s.totalOutputTokens,
			"total":  s.totalInputTokens + s.totalOutputTokens,
		},
	}

	if extra != "" {
		progressData["extra"] = extra
	}

	jsonBytes, err := json.Marshal(progressData)
	if err != nil {
		return
	}

	_, _ = fmt.Fprintf(os.Stdout, "%s\n", string(jsonBytes))
	_ = os.Stdout.Sync()
}

func (s *ProjectAnalysisSession) truncateContent(content string, maxLength int) string {
	if len(content) <= maxLength {
		return content
	}
	return content[:maxLength] + "..."
}
