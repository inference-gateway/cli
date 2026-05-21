package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	uuid "github.com/google/uuid"
	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	scheduler "github.com/inference-gateway/cli/internal/services/scheduler"
	sdk "github.com/inference-gateway/sdk"
)

const (
	scheduleOpCreate = "create"
	scheduleOpList   = "list"
	scheduleOpGet    = "get"
	scheduleOpUpdate = "update"
	scheduleOpDelete = "delete"
)

// ScheduleToolResult is the structured payload returned to the LLM.
type ScheduleToolResult struct {
	Operation string                 `json:"operation"`
	Job       *domain.ScheduledJob   `json:"job,omitempty"`
	Jobs      []*domain.ScheduledJob `json:"jobs,omitempty"`
	Message   string                 `json:"message,omitempty"`
}

// ScheduleTool lets the LLM create, inspect, and remove recurring jobs that
// are executed by the channels-manager daemon's scheduler. The tool itself
// only reads/writes YAML files under the configured storage directory; the
// daemon hot-reloads via fsnotify.
type ScheduleTool struct {
	config    *config.Config
	enabled   bool
	formatter domain.BaseFormatter
}

// NewScheduleTool creates a new Schedule tool.
func NewScheduleTool(cfg *config.Config) *ScheduleTool {
	return &ScheduleTool{
		config:    cfg,
		enabled:   cfg.Tools.Enabled && cfg.Tools.Schedule.Enabled,
		formatter: domain.NewBaseFormatter("Schedule"),
	}
}

// Definition returns the tool definition for the LLM
func (t *ScheduleTool) Definition() sdk.ChatCompletionTool {
	description := t.config.Prompts.Tools.Schedule.Description

	return sdk.ChatCompletionTool{
		Type: sdk.Function,
		Function: sdk.FunctionObject{
			Name:        "Schedule",
			Description: &description,
			Parameters: &sdk.FunctionParameters{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"operation": map[string]any{
						"type":        "string",
						"description": "The CRUD operation to perform.",
						"enum":        []string{scheduleOpCreate, scheduleOpList, scheduleOpGet, scheduleOpUpdate, scheduleOpDelete},
					},
					"job_id": map[string]any{
						"type":        "string",
						"description": "Job identifier. Required for get/update/delete; ignored for create/list.",
					},
					"cron_expression": map[string]any{
						"type":        "string",
						"description": "Standard crontab expression (5 fields) or '@every <duration>'. Required for create; optional for update.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The task to give the agent on each fire. Should be specific and self-contained - no prior context is available.",
					},
					"run_once": map[string]any{
						"type":        "boolean",
						"description": "When true, the job is deleted automatically after its first fire (one-off reminder). Default false (recurring). ALWAYS confirm with the user whether they want one-off or recurring before creating the job.",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "Optional human-friendly name for the job.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Optional longer description of the job's purpose.",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "Optional model override (e.g. 'openai/gpt-4o-mini'). Defaults to the configured agent model.",
					},
				},
				"required": []string{"operation"},
			},
		},
	}
}

// Execute runs the Schedule tool with the given arguments.
func (t *ScheduleTool) Execute(ctx context.Context, args map[string]any) (*domain.ToolExecutionResult, error) {
	start := time.Now()
	if !t.config.Tools.Enabled || !t.config.Tools.Schedule.Enabled {
		return nil, fmt.Errorf("schedule tool is not enabled")
	}

	op, _ := args["operation"].(string)
	op = strings.ToLower(strings.TrimSpace(op))

	store, err := t.openStore()
	if err != nil {
		return t.fail(args, start, fmt.Errorf("storage init: %w", err))
	}

	switch op {
	case scheduleOpCreate:
		return t.execCreate(ctx, args, store, start)
	case scheduleOpList:
		return t.execList(args, store, start)
	case scheduleOpGet:
		return t.execGet(args, store, start)
	case scheduleOpUpdate:
		return t.execUpdate(args, store, start)
	case scheduleOpDelete:
		return t.execDelete(args, store, start)
	default:
		return t.fail(args, start, fmt.Errorf("unknown operation %q (expected one of: create, list, get, update, delete)", op))
	}
}

// Validate checks if the schedule tool arguments are well-formed before
// execution. It only validates structure - semantic checks (channel exists,
// job exists) happen in Execute.
func (t *ScheduleTool) Validate(args map[string]any) error {
	if !t.config.Tools.Enabled || !t.config.Tools.Schedule.Enabled {
		return fmt.Errorf("schedule tool is not enabled")
	}
	op, ok := args["operation"].(string)
	if !ok || op == "" {
		return fmt.Errorf("operation is required")
	}
	op = strings.ToLower(strings.TrimSpace(op))

	switch op {
	case scheduleOpCreate:
		return validateCreateArgs(args)
	case scheduleOpList:
		return nil
	case scheduleOpGet, scheduleOpDelete:
		if _, err := requireString(args, "job_id"); err != nil {
			return err
		}
		return nil
	case scheduleOpUpdate:
		if _, err := requireString(args, "job_id"); err != nil {
			return err
		}
		if expr, ok := args["cron_expression"].(string); ok && expr != "" {
			if err := scheduler.ParseCron(expr); err != nil {
				return fmt.Errorf("invalid cron_expression: %w", err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown operation %q", op)
	}
}

// IsEnabled returns whether the Schedule tool is enabled.
func (t *ScheduleTool) IsEnabled() bool {
	return t.enabled
}

func validateCreateArgs(args map[string]any) error {
	expr, err := requireString(args, "cron_expression")
	if err != nil {
		return err
	}
	if err := scheduler.ParseCron(expr); err != nil {
		return fmt.Errorf("invalid cron_expression: %w", err)
	}
	if _, err := requireString(args, "prompt"); err != nil {
		return err
	}
	return nil
}

func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key].(string)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("%s is required and must be a non-empty string", key)
	}
	return v, nil
}

func optionalString(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func optionalBool(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

// openStore resolves the storage directory and constructs a Store.
func (t *ScheduleTool) openStore() (*scheduler.Store, error) {
	dir := t.config.Tools.Schedule.StorageDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		dir = filepath.Join(home, config.ConfigDirName, "schedules")
	}
	return scheduler.NewStore(dir)
}

// channelConfigured reports whether the named channel is enabled in config.
// We only check the channel's *enabled* flag - runtime registration is the
// daemon's job.
func (t *ScheduleTool) channelConfigured(name string) bool {
	switch strings.ToLower(name) {
	case "telegram":
		return t.config.Channels.Telegram.Enabled
	case "whatsapp":
		return t.config.Channels.WhatsApp.Enabled
	default:
		return false
	}
}

func (t *ScheduleTool) execCreate(ctx context.Context, args map[string]any, store *scheduler.Store, start time.Time) (*domain.ToolExecutionResult, error) {
	if err := validateCreateArgs(args); err != nil {
		return t.fail(args, start, err)
	}
	channel, recipient, err := t.resolveRouting(ctx)
	if err != nil {
		return t.fail(args, start, err)
	}
	max := t.config.Tools.Schedule.MaxJobs
	if max > 0 {
		existing, _ := store.List()
		if len(existing) >= max {
			return t.fail(args, start, fmt.Errorf("max_jobs limit (%d) reached", max))
		}
	}
	now := time.Now().UTC()
	job := &domain.ScheduledJob{
		ID:             uuid.New().String(),
		Name:           optionalString(args, "name"),
		Description:    optionalString(args, "description"),
		CronExpression: optionalString(args, "cron_expression"),
		Prompt:         optionalString(args, "prompt"),
		Channel:        channel,
		RecipientID:    recipient,
		Model:          optionalString(args, "model"),
		RunOnce:        optionalBool(args, "run_once"),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.Save(job); err != nil {
		return t.fail(args, start, err)
	}
	mode := "recurring"
	if job.RunOnce {
		mode = "one-off"
	}
	return t.success(args, start, &ScheduleToolResult{
		Operation: scheduleOpCreate,
		Job:       job,
		Message:   fmt.Sprintf("Scheduled %s job %s created. Will fire via %s while 'infer channels-manager' is running.", mode, job.ID, channel),
	})
}

// resolveRouting derives channel + recipient_id from the current session ID
// (set by cmd/agent.go via domain.WithSessionID). Channel-driven sessions are
// formatted "channel-<name>-<sender_id>" by the channels-manager. Returns an
// error when the tool is invoked outside a channel-driven session, or when
// the channel is not enabled in config.
func (t *ScheduleTool) resolveRouting(ctx context.Context) (channel, recipient string, err error) {
	sessionID := domain.GetSessionID(ctx)
	if sessionID == "" {
		return "", "", errors.New("schedule can only be used from a channel-driven session (no session id in context)")
	}
	channel, recipient, ok := domain.ParseChannelSessionID(sessionID)
	if !ok {
		return "", "", fmt.Errorf("schedule can only be used from a channel-driven session; current session %q is not channel-formatted", sessionID)
	}
	if !t.channelConfigured(channel) {
		return "", "", fmt.Errorf("channel %q is not enabled in config (enable it under channels.<name>.enabled)", channel)
	}
	return channel, recipient, nil
}

func (t *ScheduleTool) execList(args map[string]any, store *scheduler.Store, start time.Time) (*domain.ToolExecutionResult, error) {
	jobs, errs := store.List()
	if len(errs) > 0 {
		// Surface skipped files but still return what we got.
		var msgs []string
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return t.success(args, start, &ScheduleToolResult{
			Operation: scheduleOpList,
			Jobs:      jobs,
			Message:   "Some files were skipped: " + strings.Join(msgs, "; "),
		})
	}
	return t.success(args, start, &ScheduleToolResult{
		Operation: scheduleOpList,
		Jobs:      jobs,
		Message:   fmt.Sprintf("%d scheduled job(s).", len(jobs)),
	})
}

func (t *ScheduleTool) execGet(args map[string]any, store *scheduler.Store, start time.Time) (*domain.ToolExecutionResult, error) {
	id, err := requireString(args, "job_id")
	if err != nil {
		return t.fail(args, start, err)
	}
	job, err := store.Load(id)
	if err != nil {
		return t.fail(args, start, err)
	}
	return t.success(args, start, &ScheduleToolResult{
		Operation: scheduleOpGet,
		Job:       job,
	})
}

func (t *ScheduleTool) execUpdate(args map[string]any, store *scheduler.Store, start time.Time) (*domain.ToolExecutionResult, error) {
	id, err := requireString(args, "job_id")
	if err != nil {
		return t.fail(args, start, err)
	}
	job, err := store.Load(id)
	if err != nil {
		return t.fail(args, start, err)
	}
	changed := false
	if v, ok := args["cron_expression"].(string); ok && v != "" {
		if err := scheduler.ParseCron(v); err != nil {
			return t.fail(args, start, fmt.Errorf("invalid cron_expression: %w", err))
		}
		job.CronExpression = v
		changed = true
	}
	if v, ok := args["prompt"].(string); ok && v != "" {
		job.Prompt = v
		changed = true
	}
	if v, ok := args["name"].(string); ok {
		job.Name = v
		changed = true
	}
	if v, ok := args["description"].(string); ok {
		job.Description = v
		changed = true
	}
	if v, ok := args["model"].(string); ok {
		job.Model = v
		changed = true
	}
	if v, ok := args["run_once"].(bool); ok {
		job.RunOnce = v
		changed = true
	}
	if !changed {
		return t.fail(args, start, errors.New("update: no fields provided"))
	}
	job.UpdatedAt = time.Now().UTC()
	if err := store.Save(job); err != nil {
		return t.fail(args, start, err)
	}
	return t.success(args, start, &ScheduleToolResult{
		Operation: scheduleOpUpdate,
		Job:       job,
		Message:   fmt.Sprintf("Updated job %s.", id),
	})
}

func (t *ScheduleTool) execDelete(args map[string]any, store *scheduler.Store, start time.Time) (*domain.ToolExecutionResult, error) {
	id, err := requireString(args, "job_id")
	if err != nil {
		return t.fail(args, start, err)
	}
	if err := store.Delete(id); err != nil {
		return t.fail(args, start, err)
	}
	return t.success(args, start, &ScheduleToolResult{
		Operation: scheduleOpDelete,
		Message:   fmt.Sprintf("Deleted job %s.", id),
	})
}

func (t *ScheduleTool) success(args map[string]any, start time.Time, data *ScheduleToolResult) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "Schedule",
		Arguments: args,
		Success:   true,
		Duration:  time.Since(start),
		Data:      data,
	}, nil
}

func (t *ScheduleTool) fail(args map[string]any, start time.Time, err error) (*domain.ToolExecutionResult, error) {
	return &domain.ToolExecutionResult{
		ToolName:  "Schedule",
		Arguments: args,
		Success:   false,
		Duration:  time.Since(start),
		Error:     err.Error(),
	}, nil
}

// FormatResult formats tool execution results for different contexts.
func (t *ScheduleTool) FormatResult(result *domain.ToolExecutionResult, formatType domain.FormatterType) string {
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

// FormatPreview returns a compact one-line preview.
func (t *ScheduleTool) FormatPreview(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Schedule result unavailable"
	}
	data, ok := result.Data.(*ScheduleToolResult)
	if !ok {
		if result.Success {
			return "Schedule operation completed"
		}
		return "Schedule operation failed"
	}
	switch data.Operation {
	case scheduleOpCreate:
		if data.Job != nil {
			return fmt.Sprintf("Created job %s (%s)", short(data.Job.ID), data.Job.CronExpression)
		}
	case scheduleOpList:
		return fmt.Sprintf("Listed %d job(s)", len(data.Jobs))
	case scheduleOpGet:
		if data.Job != nil {
			return fmt.Sprintf("Got job %s", short(data.Job.ID))
		}
	case scheduleOpUpdate:
		if data.Job != nil {
			return fmt.Sprintf("Updated job %s", short(data.Job.ID))
		}
	case scheduleOpDelete:
		return data.Message
	}
	return data.Message
}

// FormatForUI renders the tool call header with a single-line preview.
func (t *ScheduleTool) FormatForUI(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Schedule result unavailable"
	}
	toolCall := t.formatter.FormatToolCall(result.Arguments, false)
	statusIcon := t.formatter.FormatStatusIcon(result.Success)
	preview := t.FormatPreview(result)
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n", toolCall)
	fmt.Fprintf(&out, "└─ %s %s", statusIcon, preview)
	return out.String()
}

// FormatForLLM renders a structured detail block for the assistant.
func (t *ScheduleTool) FormatForLLM(result *domain.ToolExecutionResult) string {
	if result == nil {
		return "Schedule result unavailable"
	}
	var out strings.Builder
	out.WriteString(t.formatter.FormatExpandedHeader(result))
	if result.Data != nil {
		out.WriteString(t.formatter.FormatDataSection(t.formatScheduleData(result.Data), len(result.Metadata) > 0))
	}
	out.WriteString(t.formatter.FormatExpandedFooter(result, result.Data != nil))
	return out.String()
}

func (t *ScheduleTool) formatScheduleData(data any) string {
	d, ok := data.(*ScheduleToolResult)
	if !ok {
		return t.formatter.FormatAsJSON(data)
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Operation: %s\n", d.Operation)
	if d.Message != "" {
		fmt.Fprintf(&out, "Message: %s\n", d.Message)
	}
	if d.Job != nil {
		fmt.Fprintf(&out, "Job: %s\n", d.Job.ID)
		fmt.Fprintf(&out, "Cron: %s\n", d.Job.CronExpression)
		fmt.Fprintf(&out, "Channel: %s -> %s\n", d.Job.Channel, d.Job.RecipientID)
		if d.Job.Name != "" {
			fmt.Fprintf(&out, "Name: %s\n", d.Job.Name)
		}
		if d.Job.LastRun != nil {
			fmt.Fprintf(&out, "Last run: %s\n", d.Job.LastRun.Format(time.RFC3339))
		}
		if d.Job.LastError != "" {
			fmt.Fprintf(&out, "Last error: %s\n", d.Job.LastError)
		}
	}
	if len(d.Jobs) > 0 {
		fmt.Fprintf(&out, "Jobs (%d):\n", len(d.Jobs))
		for _, j := range d.Jobs {
			fmt.Fprintf(&out, "  - %s  cron=%q  channel=%s recipient=%s\n",
				j.ID, j.CronExpression, j.Channel, j.RecipientID)
		}
	}
	return out.String()
}

// ShouldCollapseArg controls per-arg collapsing in the chat UI.
func (t *ScheduleTool) ShouldCollapseArg(key string) bool {
	return key == "prompt"
}

// ShouldAlwaysExpand keeps the result block expanded by default.
func (t *ScheduleTool) ShouldAlwaysExpand() bool {
	return false
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
