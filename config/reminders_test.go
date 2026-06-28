package config_test

import (
	"path/filepath"
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

// Reminders ship disabled by default (issue #525) with one todo-hygiene
// reminder so flipping enabled=true is enough.
func TestDefaultRemindersConfig(t *testing.T) {
	cfg := config.DefaultRemindersConfig()
	if cfg.Enabled {
		t.Error("reminders should be disabled by default")
	}
	if len(cfg.Reminders) == 0 {
		t.Fatal("default should ship one reminder")
	}
	first := cfg.Reminders[0]
	if first.Text == "" || first.Hook != domain.HookPreStream || first.Trigger != config.ReminderTriggerInterval {
		t.Errorf("unexpected default reminder: %+v", first)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default reminders config must be valid: %v", err)
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
		{"interval hit", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 4}, 4, 0, nil, true},
		{"interval miss", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 4}, 3, 0, nil, false},
		{"interval turn zero", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval, Interval: 4}, 0, 0, nil, false},
		{"interval default four", config.ReminderConfig{Name: "i", Text: "t", Trigger: config.ReminderTriggerInterval}, 8, 0, nil, true},
		{"turns_before_max inside", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 8, 10, nil, true},
		{"turns_before_max boundary", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 7, 10, nil, true},
		{"turns_before_max outside", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 6, 10, nil, false},
		{"turns_before_max no max", config.ReminderConfig{Name: "w", Text: "t", Trigger: config.ReminderTriggerTurnsBeforeMax, Threshold: 3}, 8, 0, nil, false},
		{"always", config.ReminderConfig{Name: "a", Text: "t", Trigger: config.ReminderTriggerAlways}, 1, 0, nil, true},
		{"empty trigger defaults to always", config.ReminderConfig{Name: "d", Text: "t"}, 1, 0, nil, true},
		{"once not fired", config.ReminderConfig{Name: "o", Text: "t", Trigger: config.ReminderTriggerOnce}, 5, 0, nil, true},
		{"once already fired", config.ReminderConfig{Name: "o", Text: "t", Trigger: config.ReminderTriggerOnce}, 5, 0, map[string]bool{"o": true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := remindersCfg(true, tt.reminder)
			// An empty Hook defaults to pre_stream (effective), so query that.
			got := r.RemindersDue(query(domain.HookPreStream, tt.turn, tt.maxTurns, tt.fired))
			if (len(got) > 0) != tt.want {
				t.Fatalf("RemindersDue fired=%v, want %v (got %v)", len(got) > 0, tt.want, got)
			}
		})
	}
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
