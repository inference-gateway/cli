package config

import (
	"cmp"
	"fmt"
	"slices"

	utils "github.com/inference-gateway/cli/config/utils"
	domain "github.com/inference-gateway/cli/internal/domain"
)

const (
	RemindersFileName    = "reminders.yaml"
	DefaultRemindersPath = ConfigDirName + "/" + RemindersFileName
)

// ReminderTrigger gates which firings of a hook point a reminder acts on.
type ReminderTrigger string

const (
	ReminderTriggerAlways         ReminderTrigger = "always"           // every time the hook point fires
	ReminderTriggerInterval       ReminderTrigger = "interval"         // every N turns
	ReminderTriggerTurnsBeforeMax ReminderTrigger = "turns_before_max" // within threshold of max_turns
	ReminderTriggerOnce           ReminderTrigger = "once"             // first firing of its point this run
)

// ReminderTriggers is the canonical catalog, used for config validation.
var ReminderTriggers = []ReminderTrigger{
	ReminderTriggerAlways,
	ReminderTriggerInterval,
	ReminderTriggerTurnsBeforeMax,
	ReminderTriggerOnce,
}

// Valid reports whether t is one of the pre-defined triggers.
func (t ReminderTrigger) Valid() bool { return slices.Contains(ReminderTriggers, t) }

const defaultReminderInterval = 4

const defaultTodoReminderText = `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`

// ReminderConfig is one named reminder: text injected at a pre-defined hook
// point, gated by a trigger.
type ReminderConfig struct {
	Name      string           `yaml:"name" mapstructure:"name"`
	Text      string           `yaml:"text" mapstructure:"text"`
	Hook      domain.HookPoint `yaml:"hook" mapstructure:"hook"`
	Trigger   ReminderTrigger  `yaml:"trigger" mapstructure:"trigger"`
	Interval  int              `yaml:"interval,omitempty" mapstructure:"interval"`
	Threshold int              `yaml:"threshold,omitempty" mapstructure:"threshold"`
}

// RemindersConfig is the content of reminders.yaml: the master switch plus the
// list of named reminders. Each reminder attaches to a pre-defined agent-loop
// hook point (domain.HookPoint) with a trigger. RemindersConfig implements
// domain.SystemReminderProvider. The companion executable hooks (#270) get their
// own hooks.yaml so "inject text" and "run code" stay separate concerns.
type RemindersConfig struct {
	Enabled   bool             `yaml:"enabled" mapstructure:"enabled"`
	Reminders []ReminderConfig `yaml:"reminders" mapstructure:"reminders"`
}

// DefaultRemindersConfig returns the in-code default reminders configuration
// used when no reminders.yaml exists. Reminders ship enabled by default with a
// todo-hygiene reminder and a memory-consult reminder; the latter is pruned at
// load time when memory is disabled (see pruneMemoryReminderIfDisabled) so it
// never references a feature that isn't active.
func DefaultRemindersConfig() *RemindersConfig {
	return &RemindersConfig{
		Enabled: true,
		Reminders: []ReminderConfig{
			{
				Name:     "todo-hygiene",
				Hook:     domain.HookPreStream,
				Trigger:  ReminderTriggerInterval,
				Interval: defaultReminderInterval,
				Text:     defaultTodoReminderText,
			},
			{
				Name:    "memory-consult",
				Hook:    domain.HookPreSession,
				Trigger: ReminderTriggerOnce,
				Text: `<system-reminder>
The persistent memory index (MEMORY.md) is already injected into your context. Before relying on a fact, load it in full with the Memory tool (read with its name). As you learn durable facts about the user, project, or workflow, record them with the Memory tool (write); it keeps the index in sync. Do not mention this reminder to the user.
</system-reminder>`,
			},
		},
	}
}

// LoadReminders reads reminders.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use defaults".
func LoadReminders(path string) (*RemindersConfig, error) {
	return utils.LoadYAML(path, "reminders", DefaultRemindersConfig)
}

// SaveReminders writes the reminders configuration to disk, creating any
// missing parent directories.
func SaveReminders(path string, cfg *RemindersConfig) error {
	return utils.SaveYAML(path, "reminders", cfg)
}

// effective returns the reminders with per-entry defaults applied: an empty
// Hook becomes pre_stream, an empty Trigger becomes always, and an interval
// trigger with a non-positive interval falls back to 4.
func (r RemindersConfig) effective() []ReminderConfig {
	out := make([]ReminderConfig, len(r.Reminders))
	for i, rc := range r.Reminders {
		if rc.Hook == "" {
			rc.Hook = domain.HookPreStream
		}
		if rc.Trigger == "" {
			rc.Trigger = ReminderTriggerAlways
		}
		if rc.Trigger == ReminderTriggerInterval {
			rc.Interval = cmp.Or(rc.Interval, defaultReminderInterval)
		}
		out[i] = rc
	}
	return out
}

// RemindersDue implements domain.SystemReminderProvider: it returns every
// reminder attached to q.Hook whose trigger fires. Multiple reminders on the
// same hook stack (all are returned). The interval trigger keys off
// q.SessionTurn (cumulative across the chat session) so it fires on every Nth
// conversational turn; turns_before_max keys off q.Turn/q.MaxTurns (the current
// run's loop budget). q.Fired is consulted only by the `once` trigger and is
// never written here - the caller marks names fired after injecting. A nil
// q.Fired is treated as "nothing fired yet".
func (r RemindersConfig) RemindersDue(q domain.ReminderQuery) []domain.SystemReminder {
	if !r.Enabled {
		return nil
	}
	var due []domain.SystemReminder
	for _, rc := range r.effective() {
		if rc.Hook != q.Hook {
			continue
		}
		if !reminderTriggerFires(rc, q) {
			continue
		}
		due = append(due, domain.SystemReminder{Name: rc.Name, Text: rc.Text})
	}
	return due
}

func reminderTriggerFires(rc ReminderConfig, q domain.ReminderQuery) bool {
	switch rc.Trigger {
	case ReminderTriggerInterval:
		interval := cmp.Or(rc.Interval, defaultReminderInterval)
		return q.SessionTurn > 0 && q.SessionTurn%interval == 0
	case ReminderTriggerTurnsBeforeMax:
		return q.MaxTurns > 0 && rc.Threshold > 0 && (q.MaxTurns-q.Turn) <= rc.Threshold
	case ReminderTriggerOnce:
		return !q.Fired[rc.Name]
	case ReminderTriggerAlways:
		return true
	default:
		return false
	}
}

// Validate checks each reminder against the pre-defined hook and trigger
// catalogs and the per-trigger requirements. It returns an error describing the
// first invalid entry. An empty Hook/Trigger is allowed (defaulted by
// effective); only non-empty values are checked against the catalog.
func (r RemindersConfig) Validate() error {
	for i, rc := range r.Reminders {
		switch {
		case rc.Name == "":
			return fmt.Errorf("reminders[%d]: name is required", i)
		case rc.Text == "":
			return fmt.Errorf("reminders[%d] (%s): text is required", i, rc.Name)
		case rc.Hook != "" && !rc.Hook.Valid():
			return fmt.Errorf("reminders[%d] (%s): unknown hook %q (valid: %v)", i, rc.Name, rc.Hook, domain.HookPoints)
		case rc.Trigger != "" && !rc.Trigger.Valid():
			return fmt.Errorf("reminders[%d] (%s): unknown trigger %q (valid: %v)", i, rc.Name, rc.Trigger, ReminderTriggers)
		case rc.Trigger == ReminderTriggerTurnsBeforeMax && rc.Threshold <= 0:
			return fmt.Errorf("reminders[%d] (%s): trigger turns_before_max requires threshold > 0", i, rc.Name)
		case rc.Interval < 0:
			return fmt.Errorf("reminders[%d] (%s): interval must be >= 0", i, rc.Name)
		}
	}
	return nil
}
