package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// Bounds for the AskUserQuestion schema. They are enforced in Validate (and
// re-checked in Execute) so a malformed tool call fails fast with a clear
// message instead of rendering a broken form.
const (
	minQuestions      = 1
	maxQuestions      = 4
	minOptions        = 2
	maxOptions        = 4
	maxQuestionHeader = 12
)

// AskUserQuestionTool presents the user with a small interactive form of 1-4
// multiple-choice questions during plan mode and returns the chosen answers to
// the agent so it can fold them into the plan before RequestPlanApproval.
//
// It is read-only (no approval gate) and only reaches the user when a chat TUI
// is present: the brokering capability is injected into the execution context
// only on the chat path. On headless/no-TTY runs the broker is absent and the
// tool degrades gracefully instead of blocking.
type AskUserQuestionTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewAskUserQuestionTool creates a new AskUserQuestion tool.
func NewAskUserQuestionTool(cfg *config.Config) *AskUserQuestionTool {
	return &AskUserQuestionTool{
		config:    cfg,
		enabled:   true,
		formatter: domain.NewBaseFormatter("AskUserQuestion"),
	}
}

// Definition returns the tool definition for the LLM.
func (t *AskUserQuestionTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.AskUserQuestion.Description

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "AskUserQuestion",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"additionalProperties": false,
				"type":                 "object",
				"required":             []string{"questions"},
				"properties": map[string]any{
					"questions": map[string]any{
						"type":        "array",
						"minItems":    minQuestions,
						"maxItems":    maxQuestions,
						"description": "1-4 clarifying questions to ask the user.",
						"items": map[string]any{
							"type":                 "object",
							"additionalProperties": false,
							"required":             []string{"header", "question", "options"},
							"properties": map[string]any{
								"header": map[string]any{
									"type":        "string",
									"maxLength":   maxQuestionHeader,
									"description": "Short label/chip shown as a tag (<= 12 chars).",
								},
								"question": map[string]any{
									"type":        "string",
									"description": "The full question text to display.",
								},
								"multiSelect": map[string]any{
									"type":        "boolean",
									"default":     false,
									"description": "Allow selecting multiple options.",
								},
								"options": map[string]any{
									"type":        "array",
									"minItems":    minOptions,
									"maxItems":    maxOptions,
									"description": "2-4 selectable options. An 'Other' free-text choice is always added by the UI.",
									"items": map[string]any{
										"type":                 "object",
										"additionalProperties": false,
										"required":             []string{"label", "description"},
										"properties": map[string]any{
											"label": map[string]any{
												"type":        "string",
												"description": "Concise option value returned as the answer.",
											},
											"description": map[string]any{
												"type":        "string",
												"description": "What this option means / its trade-off.",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

// Execute presents the questions to the user and returns their answers. The
// tool blocks (in its own goroutine) until the user submits, dismisses, or the
// session is cancelled. When no interactive user is reachable it returns a
// graceful result telling the model to proceed with assumptions.
func (t *AskUserQuestionTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()

	questions, err := extractQuestions(args)
	if err != nil {
		return t.failure(args, start, err.Error()), nil
	}

	broker := domain.GetUserQuestionBroker(ctx)
	if broker == nil {
		return t.result(args, start, map[string]any{
			"available": false,
			"message": "No interactive user is available to answer questions in this session " +
				"(headless/non-interactive run). Proceed using your best judgment and clearly " +
				"state the assumptions you are making, or stop and report what you need from the user.",
		}), nil
	}

	answers, ok, err := broker.AskUserQuestions(ctx, questions)
	if err != nil {
		return t.failure(args, start, fmt.Sprintf("question request cancelled: %v", err)), nil
	}
	if !ok {
		return t.result(args, start, map[string]any{
			"cancelled": true,
			"message": "The user dismissed the questions without answering. Proceed with stated " +
				"assumptions, or ask again only if essential.",
		}), nil
	}

	return t.result(args, start, map[string]any{
		"answers": answers,
		"message": formatAnswersForLLM(answers),
	}), nil
}

// Validate checks that the AskUserQuestion arguments satisfy the schema bounds.
func (t *AskUserQuestionTool) Validate(args map[string]any) error {
	_, err := extractQuestions(args)
	return err
}

// IsEnabled returns whether the AskUserQuestion tool is enabled.
func (t *AskUserQuestionTool) IsEnabled() bool {
	return t.enabled
}

func (t *AskUserQuestionTool) result(args map[string]any, start time.Time, data map[string]any) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "AskUserQuestion",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      data,
	}
}

func (t *AskUserQuestionTool) failure(args map[string]any, start time.Time, msg string) *domain.ToolExecutionResult {
	return &domain.ToolExecutionResult{
		ToolName:  "AskUserQuestion",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     msg,
	}
}

// FormatResult formats tool execution results for different contexts.
func (t *AskUserQuestionTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
	switch formatType {
	case domain.FormatterUI:
		return t.FormatForUI(result)
	case domain.FormatterLLM:
		return t.FormatForLLM(result)
	case domain.FormatterShort:
		return t.FormatPreview(result)
	default:
		return t.FormatForUI(result)
	}
}

// FormatPreview returns a short preview of the result for UI display.
func (t *AskUserQuestionTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return "AskUserQuestion failed"
	}
	switch {
	case questionResultFlag(result, "available") == flagFalse:
		return "No interactive user available"
	case questionResultFlag(result, "cancelled") == flagTrue:
		return "Questions dismissed"
	default:
		if n := answerCount(result); n > 0 {
			return fmt.Sprintf("Collected %d answer(s)", n)
		}
		return "Questions answered"
	}
}

// FormatForUI formats the result for UI display.
func (t *AskUserQuestionTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	return fmt.Sprintf("AskUserQuestion(...)\n└─ %s %s", statusIcon, t.FormatPreview(result))
}

// FormatForLLM formats the result for LLM consumption.
func (t *AskUserQuestionTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}
	if !result.Success {
		return fmt.Sprintf("Failed to ask the user: %s", result.Error)
	}
	if msg := questionResultMessage(result); msg != "" {
		return msg
	}
	return "The user answered the questions."
}

// ShouldCollapseArg determines if an argument should be collapsed in display.
func (t *AskUserQuestionTool) ShouldCollapseArg(key string) bool {
	return key == "questions"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI.
func (t *AskUserQuestionTool) ShouldAlwaysExpand() bool {
	return false
}

// triState lets the format helpers distinguish "absent" from an explicit
// boolean stored in the result Data map.
type triState int

const (
	flagAbsent triState = iota
	flagTrue
	flagFalse
)

func questionResultFlag(result *domain.ToolExecutionResult, key string) triState {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return flagAbsent
	}
	v, present := data[key].(bool)
	if !present {
		return flagAbsent
	}
	if v {
		return flagTrue
	}
	return flagFalse
}

func questionResultMessage(result *domain.ToolExecutionResult) string {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	msg, _ := data["message"].(string)
	return msg
}

func answerCount(result *domain.ToolExecutionResult) int {
	data, ok := result.Data.(map[string]any)
	if !ok {
		return 0
	}
	answers, _ := data["answers"].([]domain.UserQuestionAnswer)
	return len(answers)
}

// extractQuestions parses and validates the `questions` argument into the
// domain type. Shared by Validate and Execute so both apply identical rules.
func extractQuestions(args map[string]any) ([]domain.UserQuestion, error) {
	rawQuestions, ok := args["questions"].([]any)
	if !ok {
		return nil, fmt.Errorf("questions parameter is required and must be an array")
	}
	if len(rawQuestions) < minQuestions || len(rawQuestions) > maxQuestions {
		return nil, fmt.Errorf("questions must contain between %d and %d items, got %d", minQuestions, maxQuestions, len(rawQuestions))
	}

	questions := make([]domain.UserQuestion, 0, len(rawQuestions))
	for i, raw := range rawQuestions {
		q, err := extractQuestion(raw, i)
		if err != nil {
			return nil, err
		}
		questions = append(questions, q)
	}
	return questions, nil
}

func extractQuestion(raw any, idx int) (domain.UserQuestion, error) {
	qMap, ok := raw.(map[string]any)
	if !ok {
		return domain.UserQuestion{}, fmt.Errorf("question %d must be an object", idx+1)
	}

	header, ok := qMap["header"].(string)
	if !ok || strings.TrimSpace(header) == "" {
		return domain.UserQuestion{}, fmt.Errorf("question %d: header is required and must be a non-empty string", idx+1)
	}
	if utf8.RuneCountInString(header) > maxQuestionHeader {
		return domain.UserQuestion{}, fmt.Errorf("question %d: header must be at most %d characters", idx+1, maxQuestionHeader)
	}

	questionText, ok := qMap["question"].(string)
	if !ok || strings.TrimSpace(questionText) == "" {
		return domain.UserQuestion{}, fmt.Errorf("question %d: question text is required and must be a non-empty string", idx+1)
	}

	options, err := extractOptions(qMap["options"], idx)
	if err != nil {
		return domain.UserQuestion{}, err
	}

	multiSelect, _ := qMap["multiSelect"].(bool)

	return domain.UserQuestion{
		Header:      strings.TrimSpace(header),
		Question:    strings.TrimSpace(questionText),
		Options:     options,
		MultiSelect: multiSelect,
	}, nil
}

func extractOptions(raw any, qIdx int) ([]domain.UserQuestionOption, error) {
	rawOptions, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("question %d: options is required and must be an array", qIdx+1)
	}
	if len(rawOptions) < minOptions || len(rawOptions) > maxOptions {
		return nil, fmt.Errorf("question %d: options must contain between %d and %d items, got %d", qIdx+1, minOptions, maxOptions, len(rawOptions))
	}

	options := make([]domain.UserQuestionOption, 0, len(rawOptions))
	for j, rawOpt := range rawOptions {
		optMap, ok := rawOpt.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("question %d, option %d must be an object", qIdx+1, j+1)
		}
		label, ok := optMap["label"].(string)
		if !ok || strings.TrimSpace(label) == "" {
			return nil, fmt.Errorf("question %d, option %d: label is required and must be a non-empty string", qIdx+1, j+1)
		}
		description, _ := optMap["description"].(string)
		options = append(options, domain.UserQuestionOption{
			Label:       strings.TrimSpace(label),
			Description: strings.TrimSpace(description),
		})
	}
	return options, nil
}

// formatAnswersForLLM renders the collected answers as one line per question:
//
//	[Header] question -> label1, label2; Other: "free text"
func formatAnswersForLLM(answers []domain.UserQuestionAnswer) string {
	if len(answers) == 0 {
		return "The user provided no answers."
	}
	var b strings.Builder
	for i, a := range answers {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[%s] %s -> %s", a.Header, a.Question, formatAnswerValue(a))
	}
	return b.String()
}

func formatAnswerValue(a domain.UserQuestionAnswer) string {
	parts := make([]string, 0, 2)
	if len(a.SelectedLabels) > 0 {
		parts = append(parts, strings.Join(a.SelectedLabels, ", "))
	}
	if a.OtherText != "" {
		parts = append(parts, fmt.Sprintf("Other: %q", a.OtherText))
	}
	if len(parts) == 0 {
		return "(no selection)"
	}
	return strings.Join(parts, "; ")
}
