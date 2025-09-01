package shortcuts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	sdk "github.com/inference-gateway/sdk"
)

// PlanShortcut processes PRD files and creates GitHub issues
type PlanShortcut struct {
	agentService domain.AgentService
	config       *config.Config
}

// NewPlanShortcut creates a new plan shortcut
func NewPlanShortcut(agentService domain.AgentService, config *config.Config) *PlanShortcut {
	return &PlanShortcut{
		agentService: agentService,
		config:       config,
	}
}

func (p *PlanShortcut) GetName() string {
	return "plan"
}

func (p *PlanShortcut) GetDescription() string {
	return "Process PRD file and create GitHub issues from requirements"
}

func (p *PlanShortcut) GetUsage() string {
	return "/plan <prd_file_path>"
}

func (p *PlanShortcut) CanExecute(args []string) bool {
	return len(args) == 1
}

func (p *PlanShortcut) Execute(ctx context.Context, args []string) (ShortcutResult, error) {
	if len(args) != 1 {
		return ShortcutResult{
			Output:  "Usage: /plan <prd_file_path>",
			Success: false,
		}, nil
	}

	prdFilePath := args[0]
	if err := p.validatePRDFile(prdFilePath); err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("PRD file validation failed: %v", err),
			Success: false,
		}, nil
	}

	prdContent, err := p.readPRDFile(prdFilePath)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to read PRD file: %v", err),
			Success: false,
		}, nil
	}

	issues, err := p.processPRDWithAgent(ctx, prdContent, prdFilePath)
	if err != nil {
		return ShortcutResult{
			Output:  fmt.Sprintf("Failed to process PRD with agent: %v", err),
			Success: false,
		}, nil
	}

	if len(issues) == 0 {
		return ShortcutResult{
			Output:  "No issues were generated from the PRD",
			Success: true,
		}, nil
	}

	return ShortcutResult{
		Output:     fmt.Sprintf("📋 Successfully processed PRD and planned %d GitHub issues. Use GitHub tools to create them.", len(issues)),
		Success:    true,
		SideEffect: SideEffectNone,
		Data:       issues,
	}, nil
}

// validatePRDFile validates the PRD file path and accessibility
func (p *PlanShortcut) validatePRDFile(prdFilePath string) error {
	if prdFilePath == "" {
		return fmt.Errorf("PRD file path is required")
	}

	if !filepath.IsAbs(prdFilePath) {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get working directory: %w", err)
		}
		prdFilePath = filepath.Join(wd, prdFilePath)
	}

	fileInfo, err := os.Stat(prdFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("PRD file does not exist: %s", prdFilePath)
		}
		return fmt.Errorf("cannot access PRD file: %w", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("PRD path is a directory, not a file: %s", prdFilePath)
	}

	if fileInfo.Size() == 0 {
		return fmt.Errorf("PRD file is empty: %s", prdFilePath)
	}

	if fileInfo.Size() > 10*1024*1024 {
		return fmt.Errorf("PRD file is too large (>10MB): %s", prdFilePath)
	}

	return nil
}

// readPRDFile reads the PRD file content
func (p *PlanShortcut) readPRDFile(prdFilePath string) (string, error) {
	if !filepath.IsAbs(prdFilePath) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get working directory: %w", err)
		}
		prdFilePath = filepath.Join(wd, prdFilePath)
	}

	content, err := os.ReadFile(prdFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read PRD file: %w", err)
	}

	return string(content), nil
}

// processPRDWithAgent sends the PRD to the agent for processing
func (p *PlanShortcut) processPRDWithAgent(ctx context.Context, prdContent, prdFilePath string) ([]PlannedIssue, error) {
	messages := []sdk.Message{
		{
			Role: sdk.System,
			Content: `You are a technical project manager AI. Your task is to analyze a Product Requirements Document (PRD) and break it down into manageable GitHub issues.

For each feature or requirement in the PRD, create:
1. A clear issue title
2. A detailed description with acceptance criteria
3. Appropriate labels (feature, bug, enhancement, docs, etc.)
4. Priority level (high, medium, low)
5. Estimated effort (small, medium, large)

Respond with a JSON array of issues in this exact format:
[
  {
    "title": "Issue title",
    "description": "Detailed issue description with acceptance criteria as a checklist:\n- [ ] Criterion 1\n- [ ] Criterion 2",
    "labels": ["feature", "backend"],
    "priority": "high",
    "effort": "medium"
  }
]

Focus on creating actionable, well-defined issues that a developer can implement. Each issue should be:
- Specific and focused on one feature/requirement
- Have clear acceptance criteria
- Be appropriately sized (not too large or too small)
- Include relevant technical details from the PRD

Analyze the following PRD and create the GitHub issues:`,
		},
		{
			Role:    sdk.User,
			Content: fmt.Sprintf("PRD File: %s\n\nContent:\n%s", filepath.Base(prdFilePath), prdContent),
		},
	}

	model := p.config.Agent.Model
	if model == "" {
		model = "gpt-4"
	}

	req := &domain.AgentRequest{
		RequestID: fmt.Sprintf("plan_prd_%d", len(prdContent)),
		Model:     model,
		Messages:  messages,
	}

	eventChan, err := p.agentService.RunWithStream(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to start agent processing: %w", err)
	}

	var responseBuilder strings.Builder
	for event := range eventChan {
		switch e := event.(type) {
		case domain.ChatChunkEvent:
			responseBuilder.WriteString(e.Content)
		case domain.ChatCompleteEvent:
			response := strings.TrimSpace(responseBuilder.String())
			return p.parseIssuesFromResponse(response)
		case domain.ChatErrorEvent:
			return nil, fmt.Errorf("agent processing failed: %w", e.Error)
		}
	}

	return nil, fmt.Errorf("unexpected end of agent response stream")
}

// parseIssuesFromResponse parses the agent response to extract planned issues
func (p *PlanShortcut) parseIssuesFromResponse(response string) ([]PlannedIssue, error) {
	response = strings.TrimSpace(response)

	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")

	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, fmt.Errorf("no valid JSON array found in agent response")
	}

	jsonContent := response[jsonStart : jsonEnd+1]

	issues, err := p.parseIssueJSON(jsonContent)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issues JSON: %w", err)
	}

	return issues, nil
}

// parseIssueJSON parses JSON content into PlannedIssue structs
func (p *PlanShortcut) parseIssueJSON(jsonContent string) ([]PlannedIssue, error) {
	issues := []PlannedIssue{}

	jsonContent = strings.TrimSpace(jsonContent)
	if !strings.HasPrefix(jsonContent, "[") || !strings.HasSuffix(jsonContent, "]") {
		return nil, fmt.Errorf("JSON content is not an array")
	}

	content := strings.Trim(jsonContent, "[]")
	content = strings.TrimSpace(content)

	if content == "" {
		return issues, nil
	}

	issueStrings := p.splitJSONObjects(content)

	for i, issueStr := range issueStrings {
		issue, err := p.parseIssueObject(issueStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse issue %d: %w", i, err)
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// splitJSONObjects splits JSON array content into individual object strings
func (p *PlanShortcut) splitJSONObjects(content string) []string {
	var objects []string
	var current strings.Builder
	braceCount := 0
	inString := false
	escaped := false

	for _, char := range content {
		if escaped {
			escaped = false
			current.WriteRune(char)
			continue
		}

		if char == '\\' {
			escaped = true
			current.WriteRune(char)
			continue
		}

		if char == '"' {
			inString = !inString
		}

		if !inString {
			if char == '{' {
				braceCount++
			} else if char == '}' {
				braceCount--
			}
		}

		current.WriteRune(char)

		if !inString && braceCount == 0 && char == '}' {
			objStr := strings.TrimSpace(current.String())
			if objStr != "" {
				if strings.HasSuffix(objStr, ",") {
					objStr = strings.TrimSuffix(objStr, ",")
					objStr = strings.TrimSpace(objStr)
				}
				objects = append(objects, objStr)
			}
			current.Reset()
		}
	}

	return objects
}

// parseIssueObject parses a single JSON object string into a PlannedIssue
func (p *PlanShortcut) parseIssueObject(objStr string) (PlannedIssue, error) {
	issue := PlannedIssue{}

	objStr = strings.TrimSpace(objStr)
	if !strings.HasPrefix(objStr, "{") || !strings.HasSuffix(objStr, "}") {
		return issue, fmt.Errorf("object string is not valid JSON: %s", objStr)
	}

	content := strings.Trim(objStr, "{}")
	fields := p.parseJSONFields(content)

	for key, value := range fields {
		switch key {
		case "title":
			issue.Title = p.unquoteString(value)
		case "description":
			issue.Description = p.unquoteString(value)
		case "labels":
			issue.Labels = p.parseStringArray(value)
		case "priority":
			issue.Priority = p.unquoteString(value)
		case "effort":
			issue.Effort = p.unquoteString(value)
		}
	}

	if issue.Title == "" {
		return issue, fmt.Errorf("issue title is required")
	}

	return issue, nil
}

// parseJSONFields parses JSON object content into field map
func (p *PlanShortcut) parseJSONFields(content string) map[string]string {
	fields := make(map[string]string)

	var key, value strings.Builder
	inKey := true
	inString := false
	escaped := false
	bracketCount := 0
	braceCount := 0

	for _, char := range content {
		if escaped {
			if inKey {
				key.WriteRune(char)
			} else {
				value.WriteRune(char)
			}
			escaped = false
			continue
		}

		if char == '\\' {
			escaped = true
			if inKey {
				key.WriteRune(char)
			} else {
				value.WriteRune(char)
			}
			continue
		}

		if char == '"' {
			inString = !inString
			if inKey {
				key.WriteRune(char)
			} else {
				value.WriteRune(char)
			}
			continue
		}

		if inString {
			if inKey {
				key.WriteRune(char)
			} else {
				value.WriteRune(char)
			}
			continue
		}

		if char == '[' {
			bracketCount++
		} else if char == ']' {
			bracketCount--
		} else if char == '{' {
			braceCount++
		} else if char == '}' {
			braceCount--
		}

		if char == ':' && inKey && bracketCount == 0 && braceCount == 0 {
			inKey = false
			continue
		}

		if char == ',' && !inKey && bracketCount == 0 && braceCount == 0 {
			keyStr := strings.TrimSpace(key.String())
			valueStr := strings.TrimSpace(value.String())

			if keyStr != "" && valueStr != "" {
				fields[p.unquoteString(keyStr)] = valueStr
			}

			key.Reset()
			value.Reset()
			inKey = true
			continue
		}

		if inKey {
			key.WriteRune(char)
		} else {
			value.WriteRune(char)
		}
	}

	keyStr := strings.TrimSpace(key.String())
	valueStr := strings.TrimSpace(value.String())
	if keyStr != "" && valueStr != "" {
		fields[p.unquoteString(keyStr)] = valueStr
	}

	return fields
}

// unquoteString removes quotes from a JSON string
func (p *PlanShortcut) unquoteString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// parseStringArray parses a JSON string array
func (p *PlanShortcut) parseStringArray(arrayStr string) []string {
	arrayStr = strings.TrimSpace(arrayStr)
	if !strings.HasPrefix(arrayStr, "[") || !strings.HasSuffix(arrayStr, "]") {
		return nil
	}

	content := strings.Trim(arrayStr, "[]")
	content = strings.TrimSpace(content)

	if content == "" {
		return []string{}
	}

	var items []string
	var current strings.Builder
	inString := false
	escaped := false

	for _, char := range content {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}

		if char == '\\' {
			escaped = true
			current.WriteRune(char)
			continue
		}

		if char == '"' {
			inString = !inString
			current.WriteRune(char)
			continue
		}

		if inString {
			current.WriteRune(char)
			continue
		}

		if char == ',' {
			item := strings.TrimSpace(current.String())
			if item != "" {
				items = append(items, p.unquoteString(item))
			}
			current.Reset()
			continue
		}

		current.WriteRune(char)
	}

	item := strings.TrimSpace(current.String())
	if item != "" {
		items = append(items, p.unquoteString(item))
	}

	return items
}

// PlannedIssue represents a GitHub issue to be created from PRD analysis
type PlannedIssue struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
	Priority    string   `json:"priority"`
	Effort      string   `json:"effort"`
}
