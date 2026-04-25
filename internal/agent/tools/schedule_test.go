package tools

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
	scheduler "github.com/inference-gateway/cli/internal/services/scheduler"
)

func newScheduleCfg(t *testing.T, telegramEnabled bool) *config.Config {
	t.Helper()
	return &config.Config{
		Tools: config.ToolsConfig{
			Enabled: true,
			Schedule: config.ScheduleToolConfig{
				Enabled:    true,
				StorageDir: filepath.Join(t.TempDir(), "schedules"),
				MaxJobs:    100,
			},
		},
		Channels: config.ChannelsConfig{
			Telegram: config.TelegramChannelConfig{Enabled: telegramEnabled},
		},
	}
}

func TestScheduleTool_Definition(t *testing.T) {
	tool := NewScheduleTool(newScheduleCfg(t, true))
	def := tool.Definition()
	if def.Function.Name != "Schedule" {
		t.Errorf("expected name 'Schedule', got %s", def.Function.Name)
	}
	if def.Function.Description == nil || *def.Function.Description == "" {
		t.Error("description should not be empty")
	}
	if def.Function.Parameters == nil {
		t.Error("parameters should not be nil")
	}
}

func TestScheduleTool_IsEnabled(t *testing.T) {
	cases := []struct {
		name            string
		toolsEnabled    bool
		scheduleEnabled bool
		want            bool
	}{
		{"both enabled", true, true, true},
		{"tools off", false, true, false},
		{"schedule off", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{
				Tools: config.ToolsConfig{
					Enabled:  tc.toolsEnabled,
					Schedule: config.ScheduleToolConfig{Enabled: tc.scheduleEnabled},
				},
			}
			if got := NewScheduleTool(cfg).IsEnabled(); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestScheduleTool_Validate(t *testing.T) {
	tool := NewScheduleTool(newScheduleCfg(t, true))
	cases := []struct {
		name    string
		args    map[string]any
		wantErr bool
	}{
		{"missing operation", map[string]any{}, true},
		{"unknown operation", map[string]any{"operation": "blarg"}, true},
		{
			"create missing required",
			map[string]any{"operation": "create", "cron_expression": "* * * * *"},
			true,
		},
		{
			"create bad cron",
			map[string]any{
				"operation":       "create",
				"cron_expression": "definitely not cron",
				"prompt":          "p",
				"channel":         "telegram",
				"recipient_id":    "1",
			},
			true,
		},
		{
			"create valid",
			map[string]any{
				"operation":       "create",
				"cron_expression": "0 8 * * *",
				"prompt":          "p",
				"channel":         "telegram",
				"recipient_id":    "1",
			},
			false,
		},
		{"list", map[string]any{"operation": "list"}, false},
		{"get missing job_id", map[string]any{"operation": "get"}, true},
		{"delete missing job_id", map[string]any{"operation": "delete"}, true},
		{"update missing job_id", map[string]any{"operation": "update"}, true},
		{
			"update bad cron",
			map[string]any{"operation": "update", "job_id": "x", "cron_expression": "nope"},
			true,
		},
		{
			"update valid",
			map[string]any{"operation": "update", "job_id": "x", "cron_expression": "@every 5m"},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tool.Validate(tc.args)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestScheduleTool_Execute_Disabled(t *testing.T) {
	cfg := newScheduleCfg(t, true)
	cfg.Tools.Enabled = false
	tool := NewScheduleTool(cfg)
	if _, err := tool.Execute(context.Background(), map[string]any{"operation": "list"}); err == nil {
		t.Fatal("expected error when tool disabled")
	}
}

func TestScheduleTool_Execute_CRUDLifecycle(t *testing.T) {
	cfg := newScheduleCfg(t, true)
	tool := NewScheduleTool(cfg)
	ctx := context.Background()

	// Create
	createArgs := map[string]any{
		"operation":       "create",
		"cron_expression": "0 8 * * *",
		"prompt":          "morning quote",
		"channel":         "telegram",
		"recipient_id":    "12345",
		"name":            "morning",
	}
	r, err := tool.Execute(ctx, createArgs)
	if err != nil || !r.Success {
		t.Fatalf("create failed: err=%v result=%+v", err, r)
	}
	created, ok := r.Data.(*ScheduleToolResult)
	if !ok || created.Job == nil {
		t.Fatalf("expected ScheduleToolResult with job, got %+v", r.Data)
	}
	id := created.Job.ID

	// List should contain it
	r, _ = tool.Execute(ctx, map[string]any{"operation": "list"})
	listed := r.Data.(*ScheduleToolResult)
	if len(listed.Jobs) != 1 || listed.Jobs[0].ID != id {
		t.Fatalf("list returned wrong content: %+v", listed.Jobs)
	}

	// Get
	r, _ = tool.Execute(ctx, map[string]any{"operation": "get", "job_id": id})
	got := r.Data.(*ScheduleToolResult)
	if got.Job == nil || got.Job.ID != id || got.Job.Name != "morning" {
		t.Fatalf("get returned wrong: %+v", got.Job)
	}

	// Update prompt
	r, _ = tool.Execute(ctx, map[string]any{
		"operation": "update",
		"job_id":    id,
		"prompt":    "evening quote",
	})
	updated := r.Data.(*ScheduleToolResult)
	if updated.Job == nil || updated.Job.Prompt != "evening quote" {
		t.Fatalf("update did not change prompt: %+v", updated.Job)
	}
	if updated.Job.Channel != "telegram" {
		t.Fatalf("update accidentally changed untouched field: %+v", updated.Job)
	}

	// Delete
	r, _ = tool.Execute(ctx, map[string]any{"operation": "delete", "job_id": id})
	if !r.Success {
		t.Fatalf("delete failed: %+v", r)
	}

	// List should be empty
	r, _ = tool.Execute(ctx, map[string]any{"operation": "list"})
	listed = r.Data.(*ScheduleToolResult)
	if len(listed.Jobs) != 0 {
		t.Fatalf("expected empty list after delete, got %d", len(listed.Jobs))
	}

	// Second delete should fail
	r, _ = tool.Execute(ctx, map[string]any{"operation": "delete", "job_id": id})
	if r.Success {
		t.Fatal("delete on missing job should fail")
	}
}

func TestScheduleTool_Execute_RejectsDisabledChannel(t *testing.T) {
	cfg := newScheduleCfg(t, false) // telegram disabled
	tool := NewScheduleTool(cfg)
	r, err := tool.Execute(context.Background(), map[string]any{
		"operation":       "create",
		"cron_expression": "0 8 * * *",
		"prompt":          "x",
		"channel":         "telegram",
		"recipient_id":    "1",
	})
	if err != nil {
		t.Fatalf("expected nil error (failure encoded in result): %v", err)
	}
	if r.Success {
		t.Fatal("expected failure when channel is not enabled")
	}
}

func TestScheduleTool_Execute_MaxJobs(t *testing.T) {
	cfg := newScheduleCfg(t, true)
	cfg.Tools.Schedule.MaxJobs = 1
	tool := NewScheduleTool(cfg)
	ctx := context.Background()
	mk := func() *domain.ToolExecutionResult {
		r, _ := tool.Execute(ctx, map[string]any{
			"operation":       "create",
			"cron_expression": "0 8 * * *",
			"prompt":          "x",
			"channel":         "telegram",
			"recipient_id":    "1",
		})
		return r
	}
	if r := mk(); !r.Success {
		t.Fatalf("first create should succeed: %+v", r)
	}
	if r := mk(); r.Success {
		t.Fatal("second create should hit MaxJobs limit")
	}
}

func TestScheduleTool_Execute_GetMissing(t *testing.T) {
	tool := NewScheduleTool(newScheduleCfg(t, true))
	r, _ := tool.Execute(context.Background(), map[string]any{
		"operation": "get",
		"job_id":    "nope",
	})
	if r.Success {
		t.Fatal("get on missing job should fail")
	}
	// Verify the underlying error is ErrNotFound from the store layer.
	if !errors.Is(scheduler.ErrNotFound, scheduler.ErrNotFound) {
		t.Fatal("sanity check failed")
	}
}
