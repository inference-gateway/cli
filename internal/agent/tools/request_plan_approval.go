package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	sdk "github.com/inference-gateway/sdk"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

// plansSubdir is the directory (relative to the resolved config dir) where
// approved plan markdown files are persisted.
const plansSubdir = "plans"

// maxSlugLength caps the title-derived slug used in plan filenames so that
// long titles cannot produce unwieldy paths.
const maxSlugLength = 60

// titleSlugRegex matches sequences of characters that must be replaced with
// "-" to form a filesystem-safe slug.
var titleSlugRegex = regexp.MustCompile(`[^a-z0-9]+`)

// RequestPlanApprovalTool handles requesting plan approval from the user.
// On execute it persists the plan as a markdown file under
// "<configDir>/plans/" so users have an auditable record of every plan the
// agent produced — even when they reject it.
type RequestPlanApprovalTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
	now       func() time.Time
}

// NewRequestPlanApprovalTool creates a new RequestPlanApproval tool
func NewRequestPlanApprovalTool(cfg *config.Config) *RequestPlanApprovalTool {
	return &RequestPlanApprovalTool{
		config:    cfg,
		enabled:   true,
		formatter: domain.NewBaseFormatter("RequestPlanApproval"),
		now:       time.Now,
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
						"description": "The complete plan as Markdown. Use H2 sections (## Context, ## Files to Modify, ## Current Code, ## Changes, ## Performance Impact, ## Critical Files, ## Edge Cases, ## Verification) — include only sections that apply.",
					},
				},
			},
		},
	}
}

// Execute runs the RequestPlanApproval tool with given arguments. It writes
// the plan markdown to disk and returns the plan content plus the saved
// path so downstream consumers can surface both to the user and the LLM.
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

	path, err := t.writePlanFile(title, plan, start)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "RequestPlanApproval",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     err.Error(),
		}, nil
	}

	canonical, err := os.ReadFile(path)
	if err != nil {
		return &domain.ToolExecutionResult{
			ToolName:  "RequestPlanApproval",
			Arguments: args,
			Success:   false,
			Duration:  time.Since(start),
			Error:     fmt.Errorf("failed to read back plan file %s: %w", path, err).Error(),
		}, nil
	}

	return &domain.ToolExecutionResult{
		ToolName:  "RequestPlanApproval",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data: map[string]any{
			"title":   title,
			"plan":    string(canonical),
			"path":    path,
			"message": "Plan approval requested - waiting for user decision",
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
		if path := planPath(result); path != "" {
			return fmt.Sprintf("Plan saved to %s", path)
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
	if path := planPath(result); path != "" {
		return fmt.Sprintf("RequestPlanApproval(...)\n└─ %s Plan saved to %s", statusIcon, path)
	}
	return fmt.Sprintf("RequestPlanApproval(...)\n└─ %s Plan submitted for approval", statusIcon)
}

// FormatForLLM formats the result for LLM consumption
func (t *RequestPlanApprovalTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Tool execution result unavailable"
	}

	if result.Success {
		if path := planPath(result); path != "" {
			return fmt.Sprintf("Plan approval requested. Plan saved to %s. The user will review your plan and decide whether to accept, reject, or enable auto-approve mode.", path)
		}
		return "Plan approval requested. The user will review your plan and decide whether to accept, reject, or enable auto-approve mode."
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
	if strings.ContainsAny(title, `/\`) || strings.Contains(title, "..") {
		return "", "", fmt.Errorf("title must not contain path separators or '..'")
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

// writePlanFile materialises the plan as a markdown file in the configured
// plans directory. Writes go through a `.tmp` sibling and an os.Rename so
// the final file is never observed half-written.
func (t *RequestPlanApprovalTool) writePlanFile(title, plan string, ts time.Time) (string, error) {
	dir := filepath.Join(t.config.GetConfigDir(), plansSubdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create plans directory %s: %w", dir, err)
	}

	final, err := uniquePlanPath(dir, title, ts)
	if err != nil {
		return "", err
	}

	body := buildPlanMarkdown(title, plan)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return "", fmt.Errorf("failed to write plan file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("failed to finalise plan file %s: %w", final, err)
	}

	if abs, err := filepath.Abs(final); err == nil {
		return abs, nil
	}
	return final, nil
}

// uniquePlanPath builds a non-colliding path for a plan file in `dir`.
// Same-second titles get a `-1`, `-2`, … suffix so concurrent or rapid
// Schedule writes do not clobber each other.
func uniquePlanPath(dir, title string, ts time.Time) (string, error) {
	slug := slugifyTitle(title)
	stamp := ts.Format("2006-01-02-150405")
	base := fmt.Sprintf("%s-%s", stamp, slug)

	candidate := filepath.Join(dir, base+".md")
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to stat plan path %s: %w", candidate, err)
	}

	for i := 1; i < 1000; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d.md", base, i))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to stat plan path %s: %w", candidate, err)
		}
	}
	return "", fmt.Errorf("could not find unique plan filename in %s", dir)
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

// planPath safely extracts the persisted-file path from a tool result.
// Returns "" when the result has no path (e.g. legacy callers).
func planPath(result *domain.ToolExecutionResult) string {
	if result == nil || result.Data == nil {
		return ""
	}
	data, ok := result.Data.(map[string]any)
	if !ok {
		return ""
	}
	path, _ := data["path"].(string)
	return path
}
