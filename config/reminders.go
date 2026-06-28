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
// used when no reminders.yaml exists. Reminders ship disabled by default (issue
// #525) with one todo-hygiene reminder so flipping enabled=true is enough.
func DefaultRemindersConfig() *RemindersConfig {
	return &RemindersConfig{
		Enabled: false,
		Reminders: []ReminderConfig{
			{
				Name:     "todo-hygiene",
				Hook:     domain.HookPreStream,
				Trigger:  ReminderTriggerInterval,
				Interval: defaultReminderInterval,
				Text:     defaultTodoReminderText,
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
// reminder attached to `hook` whose trigger fires at `turn`. Multiple reminders
// on the same hook stack (all are returned). `fired` is consulted only by the
// `once` trigger and is never written here - the caller marks names fired after
// injecting. A nil `fired` is treated as "nothing fired yet".
func (r RemindersConfig) RemindersDue(
	hook domain.HookPoint, turn, maxTurns int, fired map[string]bool,
) []domain.SystemReminder {
	if !r.Enabled {
		return nil
	}
	var due []domain.SystemReminder
	for _, rc := range r.effective() {
		if rc.Hook != hook {
			continue
		}
		if !reminderTriggerFires(rc, turn, maxTurns, fired) {
			continue
		}
		due = append(due, domain.SystemReminder{Name: rc.Name, Text: rc.Text})
	}
	return due
}

func reminderTriggerFires(rc ReminderConfig, turn, maxTurns int, fired map[string]bool) bool {
	switch rc.Trigger {
	case ReminderTriggerInterval:
		interval := cmp.Or(rc.Interval, defaultReminderInterval)
		return turn > 0 && turn%interval == 0
	case ReminderTriggerTurnsBeforeMax:
		return maxTurns > 0 && rc.Threshold > 0 && (maxTurns-turn) <= rc.Threshold
	case ReminderTriggerOnce:
		return !fired[rc.Name]
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
