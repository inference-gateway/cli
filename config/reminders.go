package config

import (
	"cmp"
	"fmt"
	"os"
	"slices"

	yaml "gopkg.in/yaml.v3"

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
	ReminderTriggerAlways         ReminderTrigger = "always"
	ReminderTriggerInterval       ReminderTrigger = "interval"
	ReminderTriggerTurnsBeforeMax ReminderTrigger = "turns_before_max"
	ReminderTriggerOnce           ReminderTrigger = "once"
	ReminderTriggerOnFailure      ReminderTrigger = "on_failure"
)

// ReminderTriggers is the canonical catalog, used for config validation.
var ReminderTriggers = []ReminderTrigger{
	ReminderTriggerAlways,
	ReminderTriggerInterval,
	ReminderTriggerTurnsBeforeMax,
	ReminderTriggerOnce,
	ReminderTriggerOnFailure,
}

// Valid reports whether t is one of the pre-defined triggers.
func (t ReminderTrigger) Valid() bool { return slices.Contains(ReminderTriggers, t) }

const defaultReminderInterval = 4

// defaultMemoryReminderInterval is the cadence of the memory-hygiene reminder -
// less frequent than todo-hygiene since durable facts accrue more slowly.
const defaultMemoryReminderInterval = 10

const defaultTodoReminderText = `<system-reminder>
This is a reminder that your todo list is currently empty. DO NOT mention this to the user explicitly because they are already aware. If you are working on tasks that would benefit from a todo list please use the TodoWrite tool to create one. If not, please feel free to ignore. Again do not mention this message to the user.
</system-reminder>`

const defaultMemoryHygieneReminderText = `<system-reminder>
If you have learned durable facts about the user, project, or workflow this session - preferences, conventions, recurring gotchas, decisions worth keeping - record them now with the Memory tool (write) so they persist across sessions; it keeps the MEMORY.md index in sync. Skip if there is nothing durable to save. Do not mention this reminder to the user.
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
//
// When Merge is true, the file's reminders are merged onto the built-in defaults
// by name: a supplied entry with a built-in name overrides that entry; new names
// are appended. When false (default), the file's reminders fully replace defaults.
type RemindersConfig struct {
	Enabled   bool             `yaml:"enabled" mapstructure:"enabled"`
	Merge     bool             `yaml:"merge,omitempty" mapstructure:"merge"`
	Reminders []ReminderConfig `yaml:"reminders" mapstructure:"reminders"`
}

const defaultMemoryConsultReminderText = `<system-reminder>
The persistent memory index (MEMORY.md) is already injected into your context. Before relying on a fact, load it in full with the Memory tool (read with its name). As you learn durable facts about the user, project, or workflow, record them with the Memory tool (write); it keeps the index in sync. Do not mention this reminder to the user.
</system-reminder>`

// MemoryReminders returns the built-in reminders coupled to the memory feature:
// memory-consult (turn-1 orientation) and memory-hygiene (a periodic nudge to
// record durable facts). They are the single source of truth used to seed
// reminders.yaml (fresh init or init --overwrite) and to identify which
// reminders to prune when memory is disabled (see pruneMemoryRemindersIfDisabled).
func MemoryReminders() []ReminderConfig {
	return []ReminderConfig{
		{
			Name:    "memory-consult",
			Hook:    domain.HookPreSession,
			Trigger: ReminderTriggerOnce,
			Text:    defaultMemoryConsultReminderText,
		},
		{
			Name:     "memory-hygiene",
			Hook:     domain.HookPreStream,
			Trigger:  ReminderTriggerInterval,
			Interval: defaultMemoryReminderInterval,
			Text:     defaultMemoryHygieneReminderText,
		},
	}
}

// DefaultRemindersConfig returns the in-code default reminders configuration
// used when no reminders.yaml exists (and to seed the file on init). Reminders
// ship enabled by default with a todo-hygiene reminder plus the built-in memory
// reminders (see MemoryReminders); the memory ones are pruned at load time when
// memory is disabled (see pruneMemoryRemindersIfDisabled).
func DefaultRemindersConfig() *RemindersConfig {
	reminders := []ReminderConfig{
		{
			Name:     "todo-hygiene",
			Hook:     domain.HookPreStream,
			Trigger:  ReminderTriggerInterval,
			Interval: defaultReminderInterval,
			Text:     defaultTodoReminderText,
		},
	}
	return &RemindersConfig{
		Enabled:   true,
		Reminders: append(reminders, MemoryReminders()...),
	}
}

// LoadReminders reads reminders.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use defaults".
func LoadReminders(path string) (*RemindersConfig, error) {
	return utils.LoadYAML(path, "reminders", DefaultRemindersConfig)
}

// ParseReminders parses inline reminders YAML (e.g. the INFER_REMINDERS_CONFIG
// env var) into a RemindersConfig, so embedded consumers can supply reminders
// without writing reminders.yaml to disk. Environment references in the body are
// expanded, mirroring the file loader (LoadYAML); the result is validated by the
// caller through Config.Validate.
func ParseReminders(data []byte) (*RemindersConfig, error) {
	expanded := os.ExpandEnv(string(data))
	cfg := new(RemindersConfig)
	if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
		return nil, fmt.Errorf("failed to parse reminders config: %w", err)
	}
	return cfg, nil
}

// MergeWithDefaults returns a new RemindersConfig with the receiver's entries
// merged on top of DefaultRemindersConfig by name. A supplied entry whose Name
// matches a built-in overrides it; entries with new names are appended. The
// receiver's Enabled value is preserved so the consumer's intent wins.
func (r *RemindersConfig) MergeWithDefaults() *RemindersConfig {
	defaults := DefaultRemindersConfig()

	// Index built-in reminders by name.
	byName := make(map[string]int, len(defaults.Reminders))
	for i, def := range defaults.Reminders {
		byName[def.Name] = i
	}

	// Seed the result with built-ins, then apply overrides and appends.
	out := make([]ReminderConfig, len(defaults.Reminders))
	copy(out, defaults.Reminders)

	for _, supplied := range r.Reminders {
		if idx, ok := byName[supplied.Name]; ok {
			out[idx] = supplied // override in-place
		} else {
			out = append(out, supplied) // append new
		}
	}

	return &RemindersConfig{
		Enabled:   r.Enabled,
		Reminders: out,
	}
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
	case ReminderTriggerOnFailure:
		return q.ToolFailed
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
		case rc.Trigger == ReminderTriggerOnFailure && rc.Hook != domain.HookPostTool:
			return fmt.Errorf("reminders[%d] (%s): trigger on_failure requires hook %s", i, rc.Name, domain.HookPostTool)
		case rc.Trigger == ReminderTriggerTurnsBeforeMax && rc.Threshold <= 0:
			return fmt.Errorf("reminders[%d] (%s): trigger turns_before_max requires threshold > 0", i, rc.Name)
		case rc.Interval < 0:
			return fmt.Errorf("reminders[%d] (%s): interval must be >= 0", i, rc.Name)
		}
	}
	return nil
}
