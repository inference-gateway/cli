package shortcuts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	logger "github.com/inference-gateway/cli/internal/logger"
	sdk "github.com/inference-gateway/sdk"
)

// InitShortcut initializes AGENTS.md file by analyzing the project
type InitShortcut struct {
	config       *config.Config
	agentService domain.AgentService
	toolService  domain.ToolService
	modelService domain.ModelService
}

// NewInitShortcut creates a new init shortcut
func NewInitShortcut(cfg *config.Config, agentService domain.AgentService, toolService domain.ToolService, modelService domain.ModelService) *InitShortcut {
	return &InitShortcut{
		config:       cfg,
		agentService: agentService,
		toolService:  toolService,
		modelService: modelService,
	}
}

func (c *InitShortcut) GetName() string        { return "init" }
func (c *InitShortcut) GetDescription() string { return "Initialize AGENTS.md by analyzing the project" }
func (c *InitShortcut) GetUsage() string       { return "/init [--timeout <seconds>] [--overwrite]" }
func (c *InitShortcut) CanExecute(args []string) bool {
	// Allow optional --timeout and --overwrite flags
	return len(args) <= 4
}

func (c *InitShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	// Parse arguments
	timeout := c.config.Gateway.Timeout // Default timeout from config
	if timeout <= 0 {
		timeout = 60 // fallback default
	}
	overwrite := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--timeout":
			if i+1 < len(args) {
				var t int
				if _, err := fmt.Sscanf(args[i+1], "%d", &t); err == nil && t > 0 {
					timeout = t
				}
				i++
			}
		case "--overwrite":
			overwrite = true
		}
	}

	// Get the current model
	model := c.modelService.GetCurrentModel()
	if model == "" {
		return ShortcutResult{
			Output:  "❌ No model selected. Please select a model first using /switch or configure a default model.",
			Success: false,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("🔍 Starting project analysis with model **%s** (timeout: %ds)...\n\nThis will create an AGENTS.md file with comprehensive project documentation.", model, timeout),
		Success:    true,
		SideEffect: SideEffectInitProject,
		Data: map[string]any{
			"model":     model,
			"timeout":   timeout,
			"overwrite": overwrite,
			"context":   ctx,
		},
	}, nil
}

// PerformInit executes the actual project initialization
func (c *InitShortcut) PerformInit(ctx context.Context, model string, timeout int, overwrite bool) (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get working directory: %w", err)
	}

	agentsMDPath := "AGENTS.md"

	// Check if file already exists
	if !overwrite {
		if _, err := os.Stat(agentsMDPath); err == nil {
			return "", fmt.Errorf("AGENTS.md already exists. Use --overwrite to replace it")
		}
	}

	session := &initAnalysisSession{
		agentService:   c.agentService,
		toolService:    c.toolService,
		model:          model,
		config:         c.config,
		conversation:   []initConversationMessage{},
		maxTurns:       c.config.Agent.MaxTurns,
		startTime:      time.Now(),
		timeoutSeconds: timeout,
		agentsMDPath:   agentsMDPath,
		toolSemaphore:  make(chan struct{}, 10),
		overwrite:      overwrite,
	}

	initialPrompt := fmt.Sprintf("Please analyze the project in directory '%s' and generate a comprehensive AGENTS.md file. Use your available tools to examine the project structure, configuration files, documentation, build systems, and development workflow. Focus on creating actionable documentation that will help other AI agents understand how to work effectively with this project. Write the AGENTS.md file to: %s", wd, agentsMDPath)

	if overwrite {
		initialPrompt += fmt.Sprintf("\n\nIMPORTANT: The user has explicitly requested to overwrite the existing AGENTS.md file using the --overwrite flag. First, use the Read tool with limit=50 to read only the first 50 lines of the existing file at '%s' to get a quick overview of what's already documented. Then, use the Write tool to replace it with your updated and improved analysis. You MUST write the file even though it already exists - this is intentional.", agentsMDPath)
	}

	err = session.analyze(initialPrompt)
	if err != nil {
		return "", fmt.Errorf("analysis failed: %w", err)
	}

	return fmt.Sprintf("✅ **AGENTS.md Generated Successfully**\n\nCreated: `%s`\n\nThe file contains comprehensive project documentation to help AI agents understand and work with this project effectively.", agentsMDPath), nil
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
1. **MANDATORY FIRST STEP**: You MUST run the Tree tool as your very first action to understand the project structure. This is required before any other tool calls.
2. After running Tree, look for README files, documentation, and setup guides
3. Examine build scripts - Makefile, or Taskfile.yml
4. Check for configuration files (.gitignore, .env examples, config files)
5. Identify testing frameworks and CI/CD configurations
6. Look for code quality tools configurations
7. Use the information from Tree to guide your exploration strategy

IMPORTANT GUIDELINES:
- Be concise but comprehensive - agents need actionable information
- Focus on practical development tasks, not theoretical concepts
- Include specific commands and file paths when relevant
- Prioritize information that helps agents work effectively on the project
- Use clear, direct language without unnecessary elaboration
- If information is missing or unclear, acknowledge it rather than guessing

TOOL USAGE:
- Use available tools to explore the project (Tree, Read, Grep, etc.)
- READ IN CHUNKS: Always read files in chunks of 50 lines using the Read tool's limit and offset parameters (e.g., Read(file_path="README.md", limit=50, offset=0) for first chunk, then Read(file_path="README.md", limit=50, offset=50) for next chunk). This significantly reduces token usage.
- PARALLEL EXECUTION: You can call multiple tools simultaneously in a single response to improve efficiency. The system supports up to 10 concurrent tool executions using a semaphore-based approach. Use this to reduce back-and-forth communication by batching related operations.
- When you have gathered enough information, use the Write tool to create the AGENTS.md file
- Write the file content directly without code fences or API calls
- The Write tool expects: Write(file_path="/path/to/file", content="file content here")

EFFICIENCY TIPS:
- ALWAYS read files in 50-line chunks rather than reading entire files at once to minimize token usage
- Batch related file reads (e.g., read first 50 lines of build configuration, task files, and documentation in parallel)
- Execute multiple grep searches simultaneously for different patterns
- Combine directory exploration with file reading in the same response
- Use parallel execution to gather comprehensive project information quickly
- Only read additional chunks if the first 50 lines don't provide enough context

Your analysis should help other agents quickly understand how to work with this project effectively.`
}

// initConversationMessage represents a message in the analysis conversation
type initConversationMessage struct {
	Role       string                               `json:"role"`
	Content    string                               `json:"content"`
	ToolCalls  *[]sdk.ChatCompletionMessageToolCall `json:"tool_calls,omitempty"`
	ToolCallID string                               `json:"tool_call_id,omitempty"`
	TokenUsage *sdk.CompletionUsage                 `json:"token_usage,omitempty"`
	Timestamp  time.Time                            `json:"timestamp"`
	RequestID  string                               `json:"request_id,omitempty"`
	Internal   bool                                 `json:"-"`
}

// initAnalysisSession manages the iterative analysis session for generating AGENTS.md
type initAnalysisSession struct {
	agentService        domain.AgentService
	toolService         domain.ToolService
	model               string
	conversation        []initConversationMessage
	maxTurns            int
	completedTurns      int
	config              *config.Config
	startTime           time.Time
	timeoutSeconds      int
	totalInputTokens    int64
	totalOutputTokens   int64
	timeoutMessageCount int
	agentsMDPath        string
	toolSemaphore       chan struct{}
	conversationMutex   sync.Mutex
	overwrite           bool
}

func (s *initAnalysisSession) analyze(taskDescription string) error {
	s.addMessage(initConversationMessage{
		Role:      "user",
		Content:   taskDescription,
		Timestamp: time.Now(),
	})

	consecutiveNoToolCalls := 0
	timeoutReached := false
	fileWritten := false

	for s.completedTurns < s.maxTurns {
		if err := s.executeTurn(); err != nil {
			return err
		}

		if _, err := os.Stat(s.agentsMDPath); err == nil {
			fileWritten = true
		}

		s.completedTurns++

		if s.hasTimedOut() {
			if !timeoutReached {
				timeoutReached = true
				s.timeoutMessageCount = 1

				timeoutMsg := initConversationMessage{
					Role:      "user",
					Content:   fmt.Sprintf("TIME LIMIT REACHED. You must now complete the task immediately. Use the Write tool to create the AGENTS.md file at path '%s' with all the information you have gathered during your analysis. IMPORTANT: Write the content directly without any markdown code fences (```markdown) at the beginning or end - just write the raw markdown content. If you have been tracking todos, include any remaining incomplete tasks in a 'Future Work' or 'TODO' section so future agents know what still needs to be explored. After writing the file, do NOT make any more tool calls.", s.agentsMDPath),
					Timestamp: time.Now(),
					Internal:  false,
				}
				s.addMessage(timeoutMsg)
			} else if s.completedTurns%2 == 0 && s.timeoutMessageCount < 3 {
				s.timeoutMessageCount++

				timeoutMsg := initConversationMessage{
					Role:      "user",
					Content:   fmt.Sprintf("TIME LIMIT REACHED. You must now complete the task immediately. Use the Write tool to create the AGENTS.md file at path '%s' with all the information you have gathered during your analysis. IMPORTANT: Write the content directly without any markdown code fences (```markdown) at the beginning or end - just write the raw markdown content. If you have been tracking todos, include any remaining incomplete tasks in a 'Future Work' or 'TODO' section so future agents know what still needs to be explored. After writing the file, do NOT make any more tool calls.", s.agentsMDPath),
					Timestamp: time.Now(),
					Internal:  false,
				}
				s.addMessage(timeoutMsg)
			}
		}

		if timeoutReached && s.timeoutMessageCount >= 3 && s.completedTurns > 6 {
			break
		}

		if s.lastResponseHadNoToolCalls() {
			consecutiveNoToolCalls++

			if (consecutiveNoToolCalls >= 2 || timeoutReached) && fileWritten {
				break
			}

			if !fileWritten {
				verifyMsg := initConversationMessage{
					Role:      "user",
					Content:   fmt.Sprintf("Please use the Write tool to create the AGENTS.md file at path '%s' with the complete markdown content based on your analysis.", s.agentsMDPath),
					Timestamp: time.Now(),
					Internal:  true,
				}
				s.addMessage(verifyMsg)
			}
		} else {
			consecutiveNoToolCalls = 0
		}
	}

	if !fileWritten {
		return fmt.Errorf("agent did not write AGENTS.md file")
	}

	return nil
}

func (s *initAnalysisSession) executeTurn() error {
	ctx := context.Background()

	messages := s.buildSDKMessages()

	req := &domain.AgentRequest{
		RequestID: fmt.Sprintf("init-%d", time.Now().UnixNano()),
		Model:     s.model,
		Messages:  messages,
	}

	response, err := s.agentService.Run(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return s.processSyncResponse(response)
}

func (s *initAnalysisSession) buildSDKMessages() []sdk.Message {
	var messages []sdk.Message

	// Add system prompt at the beginning
	messages = append(messages, sdk.Message{
		Role:    sdk.System,
		Content: sdk.NewMessageContent(projectResearchSystemPrompt()),
	})

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
			Content: sdk.NewMessageContent(msg.Content),
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

func (s *initAnalysisSession) processSyncResponse(response *domain.ChatSyncResponse) error {
	if response.Usage != nil {
		s.totalOutputTokens += response.Usage.CompletionTokens
		s.totalInputTokens = response.Usage.PromptTokens
	}

	if response.Content != "" {
		assistantMsg := initConversationMessage{
			Role:       "assistant",
			Content:    response.Content,
			TokenUsage: response.Usage,
			Timestamp:  time.Now(),
		}
		s.addMessage(assistantMsg)
	}

	if len(response.ToolCalls) > 0 {
		assistantMsg := initConversationMessage{
			Role:      "assistant",
			Content:   "",
			ToolCalls: &response.ToolCalls,
			Timestamp: time.Now(),
		}
		s.addMessage(assistantMsg)

		var wg sync.WaitGroup
		toolResults := make([]initConversationMessage, len(response.ToolCalls))

		for i, toolCall := range response.ToolCalls {
			if toolCall.Id == "" {
				toolCall.Id = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), i)
			}

			wg.Add(1)
			go func(index int, tc sdk.ChatCompletionMessageToolCall) {
				defer wg.Done()

				s.toolSemaphore <- struct{}{}
				defer func() { <-s.toolSemaphore }()

				result, err := s.executeToolCall(tc.Function.Name, tc.Function.Arguments)
				if err != nil {
					logger.Error("tool execution failed",
						"tool", tc.Function.Name,
						"index", index,
						"tool_call_id", tc.Id,
						"error", err)
					toolResults[index] = initConversationMessage{
						Role:       "tool",
						Content:    fmt.Sprintf("Tool execution failed: %s", err.Error()),
						ToolCallID: tc.Id,
						Timestamp:  time.Now(),
					}
					return
				}

				toolResults[index] = initConversationMessage{
					Role:       "tool",
					Content:    s.formatToolResult(result),
					ToolCallID: tc.Id,
					Timestamp:  time.Now(),
				}
			}(i, toolCall)
		}

		wg.Wait()

		for _, toolResult := range toolResults {
			s.addMessage(toolResult)
		}
	}

	return nil
}

func (s *initAnalysisSession) executeToolCall(toolName, args string) (*domain.ToolExecutionResult, error) {
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

func (s *initAnalysisSession) formatToolResult(result *domain.ToolExecutionResult) string {
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

func (s *initAnalysisSession) addMessage(msg initConversationMessage) {
	s.conversationMutex.Lock()
	defer s.conversationMutex.Unlock()
	s.conversation = append(s.conversation, msg)
}

func (s *initAnalysisSession) lastResponseHadNoToolCalls() bool {
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

func (s *initAnalysisSession) hasTimedOut() bool {
	return time.Since(s.startTime) > time.Duration(s.timeoutSeconds)*time.Second
}
