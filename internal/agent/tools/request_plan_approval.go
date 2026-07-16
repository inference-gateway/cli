package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	storage "github.com/inference-gateway/cli/internal/infra/storage"
)

// maxSlugLength caps the title-derived slug used in plan filenames so that
// long titles cannot produce unwieldy paths.
const maxSlugLength = 60

// titleSlugRegex matches sequences of characters that must be replaced with
// "-" to form a filesystem-safe slug.
var titleSlugRegex = regexp.MustCompile(`[^a-z0-9]+`)

// RequestPlanApprovalTool handles requesting plan approval from the user.
// On execute it persists the plan to the injected PlanStorage so users have an
// auditable record of every plan the agent produced - even when they reject it.
type RequestPlanApprovalTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
	now       func() time.Time
	planStore storage.PlanStorage
}

// NewRequestPlanApprovalTool creates a new RequestPlanApproval tool. planStore
// may be nil when storage failed to initialize; Execute then fails with a
// clear error.
func NewRequestPlanApprovalTool(cfg *config.Config, planStore storage.PlanStorage) *RequestPlanApprovalTool {
	return &RequestPlanApprovalTool{
		config:    cfg,
		enabled:   true,
		formatter: domain.NewBaseFormatter("RequestPlanApproval"),
		now:       time.Now,
		planStore: planStore,
	}
}

// Definition returns the tool definition for the LLM
func (t *RequestPlanApprovalTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.RequestPlanApproval.Description

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "RequestPlanApproval",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"$schema":              "http://json-schema.org/draft-07/schema#",
				"additionalProperties": false,
				"type":                 "object",
				"required":             []string{"title", "plan"},
				"properties": map[string]any{
					"title": map[string]any{
						"type":        "string",
						"description": "A short human-readable title for the plan. Used as the H1 heading and to derive the on-disk filename.",
					},
					"plan": map[string]any{
						"type":        "string",
						"description": "The complete plan as Markdown. Use H2 sections (## Context, ## Files to Modify, ## Current Code, ## Changes, ## Performance Impact, ## Critical Files, ## Edge Cases, ## Verification) - include only sections that apply.",
					},
				},
			},
		},
	}
}

// Execute runs the RequestPlanApproval tool with given arguments. It persists
// the plan to PlanStorage and returns the plan content plus an
// infer://plans/<id> URI so downstream consumers can surface both to the user
// and the LLM.
func (t *RequestPlanApprovalTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := t.now()

	title, plan, err := extractPlanArgs(args)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "RequestPlanApproval",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	planID, err := t.savePlanToStore(ctx, title, plan, start)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "RequestPlanApproval",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	planURI := fmt.Sprintf("infer://plans/%s", planID)

	return &domain.ToolExecutionResult{
		ToolName:  "RequestPlanApproval",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"title":   title,
			"plan":    buildPlanMarkdown(title, plan),
			"plan_id": planID,
			"uri":     planURI,
			"message": fmt.Sprintf("Plan approval requested - saved as %s", planURI),
		},
	}, nil
}

// Validate checks if the RequestPlanApproval tool arguments are valid. The
// title is rejected up front when it contains path separators or ".." so we
// never even attempt to slug a malicious title.
func (t *RequestPlanApprovalTool) Validate(args map[string]any) error {
	_, _, err := extractPlanArgs(args)
	return err
}

// IsEnabled returns whether the RequestPlanApproval tool is enabled
func (t *RequestPlanApprovalTool) IsEnabled() bool {
	return t.enabled
}

// FormatResult formats tool execution results for different contexts
func (t *RequestPlanApprovalTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a short preview of the result for UI display
func (t *RequestPlanApprovalTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Success {
		if uri := planURI(result); uri != "" {
			return fmt.Sprintf("Plan saved to %s", uri)
		}
		return "Plan approval requested"
	}

	return "Plan approval request failed"
}

// FormatForUI formats the result for UI display
func (t *RequestPlanApprovalTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	if uri := planURI(result); uri != "" {
		return fmt.Sprintf("RequestPlanApproval(...)\n└─ %s Plan saved to %s", statusIcon, uri)
	}
	return fmt.Sprintf("RequestPlanApproval(...)\n└─ %s Plan submitted for approval", statusIcon)
}

// FormatForLLM formats the result for LLM consumption
func (t *RequestPlanApprovalTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Success {
		if uri := planURI(result); uri != "" {
			return fmt.Sprintf("Plan approval requested. Plan saved to %s. The user will review your plan and decide whether to accept (which enables auto-approve mode), approve each step (standard mode), or reject.", uri)
		}
		return "Plan approval requested. The user will review your plan and decide whether to accept (which enables auto-approve mode), approve each step (standard mode), or reject."
	}

	return fmt.Sprintf("Failed to request plan approval: %s", result.Error)
}

// ShouldCollapseArg determines if an argument should be collapsed in display
func (t *RequestPlanApprovalTool) ShouldCollapseArg(key string) bool {
	return key == "plan"
}

// ShouldAlwaysExpand determines if tool results should always be expanded in UI
func (t *RequestPlanApprovalTool) ShouldAlwaysExpand() bool {
	return false
}

// extractPlanArgs pulls and validates `title` and `plan` from the raw
// argument map. It is shared between Validate and Execute so both apply
// the same rules.
func extractPlanArgs(args map[string]any) (title, plan string, err error) {
	rawTitle, ok := args["title"].(string)
	if !ok {
		return "", "", fmt.Errorf("title parameter is required and must be a string")
	}
	title = strings.TrimSpace(rawTitle)
	if title == "" {
		return "", "", fmt.Errorf("title cannot be empty")
	}

	rawPlan, ok := args["plan"].(string)
	if !ok {
		return "", "", fmt.Errorf("plan parameter is required and must be a string")
	}
	plan = strings.TrimSpace(rawPlan)
	if plan == "" {
		return "", "", fmt.Errorf("plan cannot be empty")
	}
	return title, plan, nil
}

// slugifyTitle converts a free-form title into a lower-case, hyphen-separated
// slug bounded by maxSlugLength. Falls back to "plan" if the title contains
// no slug-friendly characters.
func slugifyTitle(title string) string {
	lower := strings.ToLower(title)
	slug := titleSlugRegex.ReplaceAllString(lower, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "plan"
	}
	if len(slug) > maxSlugLength {
		slug = strings.TrimRight(slug[:maxSlugLength], "-")
		if slug == "" {
			return "plan"
		}
	}
	return slug
}

// buildPlanMarkdown wraps the LLM-supplied plan body with an H1 derived from
// the title. The plan body is preserved verbatim so the model retains full
// control over the section structure.
func buildPlanMarkdown(title, plan string) string {
	var b strings.Builder
	b.WriteString("# ")
	b.WriteString(title)
	b.WriteString("\n\n")
	b.WriteString(plan)
	if !strings.HasSuffix(plan, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// savePlanToStore persists the plan to PlanStorage and returns the plan ID:
// "<UTC stamp>-<slug>", identical across backends. On jsonl the ID is also the
// markdown filename stem under <configDir>/plans/.
//
// ponytail: a second plan with the same title in the same second overwrites
// the first (upsert); resubmits after a rejection are minutes apart in practice.
func (t *RequestPlanApprovalTool) savePlanToStore(ctx context.Context, title, plan string, ts time.Time) (string, error) {
	if t.planStore == nil {
		return "", fmt.Errorf("plan storage is not available")
	}

	id := fmt.Sprintf("%s-%s", ts.UTC().Format("2006-01-02-150405"), slugifyTitle(title))
	record := &storage.PlanRecord{
		ID:        id,
		Title:     title,
		Body:      plan,
		CreatedAt: ts.UTC(),
	}

	if err := t.planStore.SavePlan(ctx, record); err != nil {
		return "", fmt.Errorf("save plan: %w", err)
	}

	return id, nil
}

// planURI safely extracts the infer://plans/<id> URI from a tool result.
// Returns "" when the result has no URI.
func planURI(result *domain.ToolExecutionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	uri, _ := data["uri"].(string)
	return uri
}
