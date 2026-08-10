package config_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	config "github.com/inference-gateway/cli/config"
	domain "github.com/inference-gateway/cli/internal/domain"
)

func TestSaveLoadReminders_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reminders.yaml")
	original := &config.RemindersConfig{
		Enabled: true,
		Reminders: []config.ReminderConfig{
			{Name: "memory", Text: "load memory", Hook: domain.HookPreSession, Trigger: config.ReminderTriggerOnce},
		},
	}
	if err := config.SaveReminders(path, original); err != nil {
		t.Fatalf("SaveReminders: %v", err)
	}
	loaded, err := config.LoadReminders(path)
	if err != nil {
		t.Fatalf("LoadReminders: %v", err)
	}
	if !loaded.Enabled || len(loaded.Reminders) != 1 ||
		loaded.Reminders[0].Name != "memory" || loaded.Reminders[0].Hook != domain.HookPreSession {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestLoadReminders_MissingFileReturnsDefaults(t *testing.T) {
	loaded, err := config.LoadReminders(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("LoadReminders on missing file: %v", err)
	}
	if len(loaded.Reminders) == 0 {
		t.Error("missing file should yield the in-code defaults")
	}
}

func remindersCfg(enabled bool, reminders ...config.ReminderConfig) config.RemindersConfig {
	return config.RemindersConfig{Enabled: enabled, Reminders: reminders}
}

// query builds a ReminderQuery with SessionTurn mirroring turn, which suffices
// for every case here: interval keys off SessionTurn, turns_before_max off
// Turn/MaxTurns, and each test exercises a single trigger.
func query(hook domain.HookPoint, turn, maxTurns int, fired map[string]bool) domain.ReminderQuery {
	return domain.ReminderQuery{Hook: hook, Turn: turn, SessionTurn: turn, MaxTurns: maxTurns, Fired: fired}
}

func TestRemindersFileConstants(t *testing.T) {
	if config.RemindersFileName != "reminders.yaml" {
		t.Errorf("RemindersFileName = %q, want reminders.yaml", config.RemindersFileName)
	}
	if config.DefaultRemindersPath != config.ConfigDirName+"/reminders.yaml" {
		t.Errorf("DefaultRemindersPath = %q", config.DefaultRemindersPath)
	}
}

// Reminders ship enabled by default with a todo-hygiene reminder plus a
// memory-consult reminder (the latter pruned at load time when memory is off).
func TestDefaultRemindersConfig(t *testing.T) {
	cfg := config.DefaultRemindersConfig()
	if !cfg.Enabled {
		t.Error("reminders should be enabled by default")
	}
	if len(cfg.Reminders) == 0 {
		t.Fatal("default should ship at least one reminder")
	}
	first := cfg.Reminders[0]
	if first.Text == "" || first.Hook != domain.HookPreStream || first.Trigger != config.ReminderTriggerInterval {
		t.Errorf("unexpected default reminder: %+v", first)
	}
	hasMemoryConsult := false
	hasUserIntentFocus := false
	for _, r := range cfg.Reminders {
		if r.Name == "memory-consult" {
			hasMemoryConsult = true
		}
		if r.Name == "user-intent-focus" {
			hasUserIntentFocus = true
		}
	}
	if !hasMemoryConsult {
		t.Error("default reminders should include memory-consult")
	}
	if !hasUserIntentFocus {
		t.Error("default reminders should include user-intent-focus")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default reminders config must be valid: %v", err)
	}
}

// memory-hygiene is a periodic (every-13-turns) nudge to record durable facts,
// mirroring todo-hygiene but less frequent; it fires on pre_stream when
// SessionTurn % 13 == 0.
func TestDefaultRemindersConfig_MemoryHygiene(t *testing.T) {
	cfg := config.DefaultRemindersConfig()

	var mh *config.ReminderConfig
	for i := range cfg.Reminders {
		if cfg.Reminders[i].Name == "memory-hygiene" {
			mh = &cfg.Reminders[i]
		}
	}
	if mh == nil {
		t.Fatal("default reminders should include memory-hygiene")
	}
	if mh.Hook != domain.HookPreStream || mh.Trigger != config.ReminderTriggerInterval || mh.Interval != 13 {
		t.Errorf("memory-hygiene should fire every 13 turns on pre_stream: %+v", *mh)
	}

	fires := func(turn int) bool {
		for _, r := range cfg.RemindersDue(query(domain.HookPreStream, turn, 0, nil)) {
			if r.Name == "memory-hygiene" {
				return true
			}
		}
		return false
	}
	if fires(1) || fires(10) {
		t.Error("memory-hygiene should not fire before turn 13")
	}
	if !fires(13) || !fires(26) {
		t.Error("memory-hygiene should fire at turns 13 and 26")
	}
}

func TestRemindersDue_MasterGateDisabled(t *testing.T) {
	r := remindersCfg(false, config.ReminderConfig{
		Name: "a", Text: "x", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways,
	})
	if got := r.RemindersDue(query(domain.HookPreStream, 1, 0, nil)); got != nil {
		t.Fatalf("disabled config must return nil, got %v", got)
	}
}

func TestRemindersDue_Triggers(t *testing.T) {
	tests := []struct {
		name     string
		reminder config.ReminderConfig
		turn     int
		maxTurns int
		fired    map[string]bool
		want     bool
	}{
		{"interval hit", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 10}, 10, 0, nil, true},
		{"interval miss", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 10}, 3, 0, nil, false},
		{"interval turn zero", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 10}, 0, 0, nil, false},
		{"interval default ten", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval}, 10, 0, nil, true},
		{"turns_before_max inside", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 8, 10, nil, true},
		{"turns_before_max boundary", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 7, 10, nil, true},
		{"turns_before_max outside", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 6, 10, nil, false},
		{"turns_before_max no max", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 8, 0, nil, false},
		{"turns_before_max already fired", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 8, 10, map[string]bool{"w": true}, false},
		{"always", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTriggerAlways}, 1, 0, nil, true},
		{"empty trigger defaults to always", config.ReminderConfig{Name: "d", Text: "t"}, 1, 0, nil, true},
		{"once not fired", config.ReminderConfig{Name: "o", Text: "t", Trigger: config.ReminderTriggerOnce}, 5, 0, nil, true},
		{"once already fired", config.ReminderConfig{Name: "o", Text: "t", Trigger: config.ReminderTriggerOnce}, 5, 0, map[string]bool{"o": true}, false},
		{"once_after before threshold", config.ReminderConfig{Name: "oa", Text: "t", Trigger: config.ReminderTriggerOnceAfter, Threshold: 3}, 2, 0, nil, false},
		{"once_after at threshold", config.ReminderConfig{Name: "oa", Text: "t", Trigger: config.ReminderTriggerOnceAfter, Threshold: 3}, 3, 0, nil, true},
		{"once_after past threshold", config.ReminderConfig{Name: "oa", Text: "t", Trigger: config.ReminderTriggerOnceAfter, Threshold: 3}, 5, 0, nil, true},
		{"once_after already fired", config.ReminderConfig{Name: "oa", Text: "t", Trigger: config.ReminderTriggerOnceAfter, Threshold: 3}, 5, 0, map[string]bool{"oa": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := remindersCfg(true, tt.reminder)
			got := r.RemindersDue(query(domain.HookPreStream, tt.turn, tt.maxTurns, tt.fired))
			if (len(got) > 0) != tt.want {
				t.Fatalf("RemindersDue fired=%v, want %v (got %v)", len(got) > 0, tt.want, got)
			}
		})
	}
}

func TestRemindersDue_WhenTodosEmpty(t *testing.T) {
	reminder := config.ReminderConfig{
		Name: "todo-hygiene", Text: "t",
		Trigger: config.ReminderTriggerInterval, Interval: 4,
		When: config.ReminderWhenTodosEmpty,
	}
	tests := []struct {
		name      string
		todoCount int
		want      bool
	}{
		{"fires while list empty", 0, true},
		{"skipped while list has items", 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := remindersCfg(true, reminder)
			q := query(domain.HookPreStream, 4, 0, nil)
			q.TodoCount = tt.todoCount
			if got := r.RemindersDue(q); (len(got) > 0) != tt.want {
				t.Fatalf("RemindersDue fired=%v, want %v", len(got) > 0, tt.want)
			}
		})
	}

	t.Run("unknown when never fires", func(t *testing.T) {
		bad := reminder
		bad.When = "todos_emtpy"
		r := remindersCfg(true, bad)
		if got := r.RemindersDue(query(domain.HookPreStream, 4, 0, nil)); len(got) != 0 {
			t.Fatalf("unknown when fired: %v", got)
		}
	})
}

func TestRemindersDue_HookFiltering(t *testing.T) {
	r := remindersCfg(true,
		config.ReminderConfig{Name: "pre", Text: "p", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways},
		config.ReminderConfig{Name: "post", Text: "q", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerAlways},
	)
	pre := r.RemindersDue(query(domain.HookPreStream, 1, 0, nil))
	if len(pre) != 1 || pre[0].Name != "pre" {
		t.Fatalf("pre_stream should return only the pre reminder, got %v", pre)
	}
	post := r.RemindersDue(query(domain.HookPostTool, 1, 0, nil))
	if len(post) != 1 || post[0].Name != "post" {
		t.Fatalf("post_tool should return only the post reminder, got %v", post)
	}
	if got := r.RemindersDue(query(domain.HookPostSession, 1, 0, nil)); got != nil {
		t.Fatalf("post_session has no reminders, got %v", got)
	}
}

// Multiple reminders on the same hook all fire (they stack) - the headline
// capability of the new model over the old single-reminder design.
func TestRemindersDue_Stacking(t *testing.T) {
	r := remindersCfg(true,
		config.ReminderConfig{Name: "todo", Text: "t", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways},
		config.ReminderConfig{Name: "memory", Text: "m", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways},
	)
	got := r.RemindersDue(query(domain.HookPreStream, 1, 0, nil))
	if len(got) != 2 {
		t.Fatalf("both reminders on pre_stream should fire, got %v", got)
	}
}

// The `once` trigger fires the first time and is suppressed once the caller has
// recorded the name in the shared fired-set.
func TestRemindersDue_OnceAcrossCalls(t *testing.T) {
	r := remindersCfg(true, config.ReminderConfig{
		Name: "memory", Text: "m", Hook: domain.HookPreSession, Trigger: config.ReminderTriggerOnce,
	})
	fired := map[string]bool{}

	first := r.RemindersDue(query(domain.HookPreSession, 1, 0, fired))
	if len(first) != 1 {
		t.Fatalf("once reminder should fire first time, got %v", first)
	}
	fired[first[0].Name] = true // the agent marks fired after injecting

	if got := r.RemindersDue(query(domain.HookPreSession, 2, 0, fired)); got != nil {
		t.Fatalf("once reminder should be suppressed after firing, got %v", got)
	}
}

// on_failure fires at post_tool only when the just-completed tool batch failed
// (q.ToolFailed), so an embedded consumer can nudge the model only when a change
// did not happen instead of paying the always-on injection cost.
func TestRemindersDue_OnFailure(t *testing.T) {
	r := remindersCfg(true, config.ReminderConfig{
		Name: "fail-nudge", Text: "the change did not happen",
		Hook: domain.HookPostTool, Trigger: config.ReminderTriggerOnFailure,
	})

	failed := domain.ReminderQuery{Hook: domain.HookPostTool, ToolFailed: true}
	if got := r.RemindersDue(failed); len(got) != 1 || got[0].Name != "fail-nudge" {
		t.Fatalf("on_failure should fire when a tool failed, got %v", got)
	}

	ok := domain.ReminderQuery{Hook: domain.HookPostTool, ToolFailed: false}
	if got := r.RemindersDue(ok); got != nil {
		t.Fatalf("on_failure must not fire when no tool failed, got %v", got)
	}
}

// ParseReminders lets embedded consumers (INFER_REMINDERS_CONFIG) supply the
// reminders config inline instead of writing reminders.yaml; it expands env
// references in the body like the file loader.
func TestParseReminders(t *testing.T) {
	cfg, err := config.ParseReminders([]byte(`enabled: true
reminders:
  - name: fail-nudge
    hook: post_tool
    trigger: on_failure
    text: boom
`))
	if err != nil {
		t.Fatalf("ParseReminders(valid): %v", err)
	}
	if !cfg.Enabled || len(cfg.Reminders) != 1 ||
		cfg.Reminders[0].Trigger != config.ReminderTriggerOnFailure || cfg.Reminders[0].Hook != domain.HookPostTool {
		t.Fatalf("unexpected parse result: %+v", cfg)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("parsed config should validate: %v", err)
	}

	if _, err := config.ParseReminders([]byte("enabled: true\nreminders: [unterminated")); err == nil {
		t.Error("malformed YAML should error")
	}

	t.Setenv("REMINDER_TEXT_FIXTURE", "expanded-text")
	exp, err := config.ParseReminders([]byte("enabled: true\nreminders:\n  - name: n\n    text: ${REMINDER_TEXT_FIXTURE}\n"))
	if err != nil {
		t.Fatalf("ParseReminders(env): %v", err)
	}
	if len(exp.Reminders) != 1 || exp.Reminders[0].Text != "expanded-text" {
		t.Fatalf("env expansion failed: %+v", exp.Reminders)
	}
}

func TestMergeWithDefaults_AppendsNew(t *testing.T) {
	cfg := config.RemindersConfig{
		Enabled: true,
		Merge:   true,
		Reminders: []config.ReminderConfig{
			{Name: "my-custom", Text: "do the thing", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways},
		},
	}
	merged := cfg.MergeWithDefaults()
	if !merged.Enabled {
		t.Error("merged config should keep Enabled=true")
	}
	wantLen := len(config.DefaultRemindersConfig().Reminders) + 1
	if len(merged.Reminders) != wantLen {
		t.Fatalf("expected %d reminders (defaults + 1), got %d", wantLen, len(merged.Reminders))
	}
	found := false
	for _, r := range merged.Reminders {
		if r.Name == "my-custom" {
			found = true
			if r.Text != "do the thing" {
				t.Errorf("custom reminder text = %q, want %q", r.Text, "do the thing")
			}
			break
		}
	}
	if !found {
		t.Error("custom reminder 'my-custom' not found in merged result")
	}
	hasTodo := false
	hasMemoryConsult := false
	hasMemoryHygiene := false
	hasUserIntentFocus := false
	for _, r := range merged.Reminders {
		switch r.Name {
		case "todo-hygiene":
			hasTodo = true
		case "memory-consult":
			hasMemoryConsult = true
		case "memory-hygiene":
			hasMemoryHygiene = true
		case "user-intent-focus":
			hasUserIntentFocus = true
		}
	}
	if !hasTodo {
		t.Error("merged result should include todo-hygiene")
	}
	if !hasMemoryConsult {
		t.Error("merged result should include memory-consult")
	}
	if !hasMemoryHygiene {
		t.Error("merged result should include memory-hygiene")
	}
	if !hasUserIntentFocus {
		t.Error("merged result should include user-intent-focus")
	}
}

func TestMergeWithDefaults_OverridesByName(t *testing.T) {
	cfg := config.RemindersConfig{
		Enabled: true,
		Merge:   true,
		Reminders: []config.ReminderConfig{
			{Name: "todo-hygiene", Text: "overridden text", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerAlways},
		},
	}
	merged := cfg.MergeWithDefaults()
	if !merged.Enabled {
		t.Error("merged config should keep Enabled=true")
	}
	wantLen := len(config.DefaultRemindersConfig().Reminders)
	if len(merged.Reminders) != wantLen {
		t.Fatalf("expected %d reminders (same as defaults), got %d", wantLen, len(merged.Reminders))
	}
	for i, r := range merged.Reminders {
		if r.Name == "todo-hygiene" {
			if r.Text != "overridden text" {
				t.Errorf("todo-hygiene text = %q, want %q", r.Text, "overridden text")
			}
			if i != 0 {
				t.Errorf("todo-hygiene should remain at index 0, got %d", i)
			}
			break
		}
	}
	hasMemoryConsult := false
	hasUserIntentFocus := false
	for _, r := range merged.Reminders {
		if r.Name == "memory-consult" {
			hasMemoryConsult = true
		}
		if r.Name == "user-intent-focus" {
			hasUserIntentFocus = true
		}
	}
	if !hasMemoryConsult {
		t.Error("merged result should include memory-consult")
	}
	if !hasUserIntentFocus {
		t.Error("merged result should include user-intent-focus")
	}
}

func TestMergeWithDefaults_PreservesEnabled(t *testing.T) {
	cfg := config.RemindersConfig{
		Enabled: false,
		Merge:   true,
		Reminders: []config.ReminderConfig{
			{Name: "custom", Text: "x"},
		},
	}
	merged := cfg.MergeWithDefaults()
	if merged.Enabled {
		t.Error("merged config should keep Enabled=false from the receiver")
	}
}

func TestParseReminders_ReplaceIsDefault(t *testing.T) {
	cfg, err := config.ParseReminders([]byte(`enabled: true
reminders:
  - name: only-this
    text: only this reminder
`))
	if err != nil {
		t.Fatalf("ParseReminders: %v", err)
	}
	if cfg.Merge {
		t.Error("merge should default to false when not specified")
	}
	if len(cfg.Reminders) != 1 || cfg.Reminders[0].Name != "only-this" {
		t.Fatalf("expected only the supplied reminder, got %+v", cfg.Reminders)
	}
}

func TestReminders_Validate(t *testing.T) {
	tests := []struct {
		name      string
		reminder  config.ReminderConfig
		wantError bool
	}{
		{"valid", config.ReminderConfig{Name: "a", Text: "t", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerInterval, Interval: 4}, false},
		{"empty hook and trigger default ok", config.ReminderConfig{Name: "a", Text: "t"}, false},
		{"missing name", config.ReminderConfig{Text: "t", Hook: domain.HookPreStream}, true},
		{"missing text", config.ReminderConfig{Name: "a", Hook: domain.HookPreStream}, true},
		{"unknown hook", config.ReminderConfig{Name: "a", Text: "t", Hook: domain.HookPoint("not_a_hook")}, true},
		{"unknown trigger", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTrigger("nope")}, true},
		{"turns_before_max needs threshold", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax}, true},
		{"negative interval", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: -1}, true},
		{"on_failure with post_tool ok", config.ReminderConfig{Name: "a", Text: "t", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerOnFailure}, false},
		{"on_failure rejects other hook", config.ReminderConfig{Name: "a", Text: "t", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerOnFailure}, true},
		{"on_failure requires explicit hook", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTriggerOnFailure}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := remindersCfg(true, tt.reminder)
			err := r.Validate()
			if (err != nil) != tt.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestOnStalledTodosTrigger(t *testing.T) {
	todos := []domain.TodoItem{
		{ID: "1", Content: "wire modalities", Status: "pending"},
		{ID: "2", Content: "open draft PR", Status: "in_progress"},
	}
	tests := []struct {
		name      string
		threshold int
		todos     []domain.TodoItem
		strikes   int
		want      bool
	}{
		{"first strike fires", 0, todos, 1, true},
		{"second strike fires", 0, todos, 2, true},
		{"third strike capped by default threshold", 0, todos, 3, false},
		{"no incomplete todos is silent", 0, nil, 1, false},
		{"overridden threshold extends the cap", 5, todos, 4, true},
		{"overridden threshold still caps", 2, todos, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := remindersCfg(true, config.ReminderConfig{
				Name:      "todo-continuation",
				Hook:      domain.HookPostStream,
				Trigger:   config.ReminderTriggerOnStalledTodos,
				Threshold: tt.threshold,
				Text:      "incomplete:\n{todo_list}",
			})
			q := query(domain.HookPostStream, 1, 10, nil)
			q.IncompleteTodos = tt.todos
			q.StalledStrikes = tt.strikes
			due := cfg.RemindersDue(q)
			if fired := len(due) == 1; fired != tt.want {
				t.Fatalf("fired = %v, want %v (due: %+v)", fired, tt.want, due)
			}
			if tt.want {
				text := due[0].Text
				for _, want := range []string{"- [pending] wire modalities", "- [in_progress] open draft PR"} {
					if !strings.Contains(text, want) {
						t.Errorf("resolved text missing %q:\n%s", want, text)
					}
				}
			}
		})
	}
}

func TestOnTruncationTrigger(t *testing.T) {
	tests := []struct {
		name         string
		hook         domain.HookPoint
		finishReason string
		want         bool
	}{
		{"fires on length at post_stream", domain.HookPostStream, "length", true},
		{"silent on stop", domain.HookPostStream, "stop", false},
		{"silent on empty finish reason", domain.HookPostStream, "", false},
		{"silent at other hooks", domain.HookPostTool, "length", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := remindersCfg(true, config.ReminderConfig{
				Name:    "truncation-continuation",
				Hook:    domain.HookPostStream,
				Trigger: config.ReminderTriggerOnTruncation,
				Text:    "continue where you left off",
			})
			q := query(tt.hook, 1, 10, nil)
			q.FinishReason = tt.finishReason
			if fired := len(cfg.RemindersDue(q)) == 1; fired != tt.want {
				t.Fatalf("fired = %v, want %v", fired, tt.want)
			}
		})
	}
}

func TestOnRepeatedFailureTrigger(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		want     bool
	}{
		{"below threshold is silent", 2, false},
		{"at threshold fires", 3, true},
		{"above threshold fires", 5, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := remindersCfg(true, config.ReminderConfig{
				Name:      "repeated-failure",
				Hook:      domain.HookPostTool,
				Trigger:   config.ReminderTriggerOnRepeatedFailure,
				Threshold: 3,
				Text:      "{tool_name} failed {count} times",
			})
			q := query(domain.HookPostTool, 1, 10, nil)
			q.RepeatedFailures = tt.failures
			q.FailedTool = "Read"
			due := cfg.RemindersDue(q)
			if fired := len(due) == 1; fired != tt.want {
				t.Fatalf("fired = %v, want %v", fired, tt.want)
			}
			if tt.want {
				if want := fmt.Sprintf("Read failed %d times", tt.failures); due[0].Text != want {
					t.Errorf("resolved text = %q, want %q", due[0].Text, want)
				}
				if !due[0].AppendToToolResult {
					t.Error("on_repeated_failure reminders must set AppendToToolResult")
				}
			}
		})
	}
}

func TestValidateNewTriggerRequirements(t *testing.T) {
	tests := []struct {
		name      string
		reminder  config.ReminderConfig
		wantError bool
	}{
		{"on_repeated_failure at wrong hook", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostStream, Trigger: config.ReminderTriggerOnRepeatedFailure, Threshold: 3, Text: "t"}, true},
		{"on_repeated_failure without threshold", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerOnRepeatedFailure, Text: "t"}, true},
		{"on_repeated_failure valid", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerOnRepeatedFailure, Threshold: 3, Text: "t"}, false},
		{"on_truncation at wrong hook", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostTool, Trigger: config.ReminderTriggerOnTruncation, Text: "t"}, true},
		{"on_truncation valid", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostStream, Trigger: config.ReminderTriggerOnTruncation, Text: "t"}, false},
		{"on_stalled_todos at wrong hook", config.ReminderConfig{
			Name: "r", Hook: domain.HookPreStream, Trigger: config.ReminderTriggerOnStalledTodos, Text: "t"}, true},
		{"on_stalled_todos valid", config.ReminderConfig{
			Name: "r", Hook: domain.HookPostStream, Trigger: config.ReminderTriggerOnStalledTodos, Text: "t"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := remindersCfg(true, tt.reminder)
			if err := cfg.Validate(); (err != nil) != tt.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestDefaultRemindersSeedContinuationBuiltins(t *testing.T) {
	names := map[string]bool{}
	for _, r := range config.DefaultRemindersConfig().Reminders {
		names[r.Name] = true
	}
	for _, want := range []string{"repeated-failure", "todo-continuation", "truncation-continuation"} {
		if !names[want] {
			t.Errorf("DefaultRemindersConfig missing built-in %q", want)
		}
	}
}
