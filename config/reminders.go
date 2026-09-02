package config

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	yaml "gopkg.in/yaml.v3"

	configutils "github.com/inference-gateway/cli/config/utils"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
)

const (
	RemindersFileName    = "reminders.yaml"
	DefaultRemindersPath = ConfigDirName + "/" + RemindersFileName
)

// ReminderTrigger gates which firings of a hook point a reminder acts on.
type ReminderTrigger string

const (
	ReminderTriggerAlways            ReminderTrigger = "always"
	ReminderTriggerInterval          ReminderTrigger = "interval"
	ReminderTriggerOnceAfter         ReminderTrigger = "once_after"
	ReminderTriggerTurnsBeforeMax    ReminderTrigger = "turns_before_max"
	ReminderTriggerOnce              ReminderTrigger = "once"
	ReminderTriggerOnFailure         ReminderTrigger = "on_failure"
	ReminderTriggerOnModeChange      ReminderTrigger = "on_mode_change"
	ReminderTriggerOnRepeatedFailure ReminderTrigger = "on_repeated_failure"
	ReminderTriggerOnTruncation      ReminderTrigger = "on_truncation"
	ReminderTriggerOnStalledTodos    ReminderTrigger = "on_stalled_todos"
)

// ReminderTriggers is the canonical catalog, used for config validation.
var ReminderTriggers = []ReminderTrigger{
	ReminderTriggerAlways,
	ReminderTriggerInterval,
	ReminderTriggerOnceAfter,
	ReminderTriggerTurnsBeforeMax,
	ReminderTriggerOnce,
	ReminderTriggerOnFailure,
	ReminderTriggerOnModeChange,
	ReminderTriggerOnRepeatedFailure,
	ReminderTriggerOnTruncation,
	ReminderTriggerOnStalledTodos,
}

// Valid reports whether t is one of the pre-defined triggers.
func (t ReminderTrigger) Valid() bool { return slices.Contains(ReminderTriggers, t) }

const defaultReminderInterval = 10

// defaultMemoryReminderInterval is the cadence of the memory-hygiene reminder -
// less frequent than todo-hygiene since durable facts accrue more slowly.
const defaultMemoryReminderInterval = 13

// defaultUserIntentFocusThreshold is the turn threshold for the
// user-intent-focus reminder - fires once after 3 turns.
const defaultUserIntentFocusThreshold = 3

const defaultTodoReminderText = `<system-reminder>
This is a reminder to keep your todo list current. If you are working on tasks that would benefit from a todo list, use the TodoWrite tool to create one or update it as you make progress. If not, please feel free to ignore. DO NOT mention this message to the user.
</system-reminder>`

const defaultMemoryHygieneReminderText = `<system-reminder>
If you have learned durable facts about the user, project, or workflow this session - preferences, conventions, recurring gotchas, decisions worth keeping - record them now with the Memory tool (write) so they persist across sessions; it keeps the MEMORY.md index in sync. Skip if there is nothing durable to save. Do not mention this reminder to the user.
</system-reminder>`

const defaultUserIntentFocusReminderText = `<system-reminder>
Focus on the user's initial explicit instructions - their own words are your primary directive, not the context or background in the issue/PR body. If the user says "DO NOT implement yet", "just create the issue", or any other explicit constraint, follow it exactly and stop there. DO NOT mention this message to the user.
</system-reminder>`

const defaultRepeatedFailureReminderText = `<system-reminder>
{tool_name} failed {count} times with identical arguments. Stop retrying - verify your assumptions (list or search first) and take a different approach.
</system-reminder>`

const defaultTodoContinuationReminderText = `<system-reminder>
This is an automated check, not a message from the user. Your todo list still has incomplete items:
{todo_list}Your last reply had no tool calls, which ends the session. Continue working on the next incomplete item now, using tool calls. If an item is already done or obsolete, update the list via TodoWrite (mark it completed or remove it) before stopping. Do not reply with prose only.
</system-reminder>`

const defaultTruncationContinuationReminderText = `<system-reminder>
This is an automated check, not a message from the user. Your previous response was truncated by the token limit before any tool call was emitted. Continue where you left off: keep the reply short and re-issue the intended tool call.
</system-reminder>`

// defaultRepeatedFailureThreshold is the built-in threshold for the
// on_repeated_failure trigger (fires from the 3rd identical failure on).
const defaultRepeatedFailureThreshold = 3

// defaultStalledTodosThreshold caps the consecutive no-tool-call strikes
// before the on_stalled_todos trigger stops firing (3-strike cap from #946).
const defaultStalledTodosThreshold = 3

// ReminderConfig is one named reminder: text injected at a pre-defined hook
// point, gated by a trigger.
//
// Guidance is consulted only by the on_mode_change trigger: it maps a mode key
// ("standard"/"plan"/"auto", the same keys as tools.bash.mode.<key>) to the
// text substituted for the {guidance} placeholder when that mode is entered.
// Since the mode-change reminder is the sole carrier of per-mode instructions
// (the system prompt stays byte-stable across mode switches for the KV cache),
// the default texts below carry the full mode behaviour the old per-mode
// system prompts used to hold. Keys the user omits keep their built-in
// defaults (per-key merge with effective()).
type ReminderConfig struct {
	Name      string                `yaml:"name" mapstructure:"name"`
	Text      string                `yaml:"text" mapstructure:"text"`
	Hook      agentdomain.HookPoint `yaml:"hook" mapstructure:"hook"`
	Trigger   ReminderTrigger       `yaml:"trigger" mapstructure:"trigger"`
	Interval  int                   `yaml:"interval,omitempty" mapstructure:"interval"`
	Threshold int                   `yaml:"threshold,omitempty" mapstructure:"threshold"`
	Guidance  map[string]string     `yaml:"guidance,omitempty" mapstructure:"guidance"`
	When      string                `yaml:"when,omitempty" mapstructure:"when"`
}

// ReminderWhenTodosEmpty gates a reminder to fire only while the session's
// todo list has no items at all.
const ReminderWhenTodosEmpty = "todos_empty"

// RemindersConfig is the content of reminders.yaml: the master switch plus the
// list of named reminders. Each reminder attaches to a pre-defined agent-loop
// hook point (agentdomain.HookPoint) with a trigger. RemindersConfig implements
// agentdomain.SystemReminderProvider. The companion executable hooks (#270) get their
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

const DefaultModeChangeReminderName = "mode-change-reminder"

const DefaultModeChangeReminderText = `<system-reminder>
The agent mode has changed mid-session from {prev_mode} to {new_mode}. {guidance} Adapt your behavior to the new mode for the rest of this session.
</system-reminder>`

// defaultModeChangeGuidance is the built-in {guidance} text per target-mode
// key. Mode adjustments - the old per-mode system prompts (restrictions,
// workflow, tool set, plan format, destructive-action policy) - live here so
// users override them in one place: the reminder's guidance map in
// reminders.yaml. prompts.yaml's agent.mode_adjustment_plan/_auto act as
// per-mode overrides layered through ReminderQuery.ModeGuidance when set
// (see resolveModeChangeText).
var defaultModeChangeGuidance = map[string]string{
	"plan": "You are now in Plan Mode: a read-only mode. Analyze the user requests and create ACTIONABLE, EXECUTABLE plans WITHOUT executing them. " +
		"TOOL SET: only Read, Grep, Tree, TodoWrite, AskUserQuestion, RequestPlanApproval, A2A_QueryAgent, and Wait remain executable - " +
		"Write, Edit, MultiEdit, Delete, Bash, and the web/other tools stay listed in the tool-use API but are DISABLED in plan mode and any call to them returns an error; do NOT attempt to make changes to files or the system, and do NOT attempt to implement the plan. " +
		"WORKFLOW: investigate with the read-only tools until you understand the codebase; identify ALL requirements; " +
		"if a decision hinges on a discrete choice (approach, scope, format, naming, trade-off), call AskUserQuestion (1-4 multiple-choice questions, 2-4 options each) instead of guessing, and ask open-ended questions in a regular assistant turn; " +
		"iterate until the plan is complete, then call RequestPlanApproval with a short title AND the Markdown plan body. Plans that are not actionable are NOT plans - if accepted, YOU will execute it step-by-step. " +
		"PLAN FORMAT: the 'plan' argument MUST be a Markdown document using these H2 sections, in this order, omitting the ones that do not apply and never inventing extra top-level sections: " +
		"## Context (why the change is being made); ## Files to Modify (exact paths, one-line note each); ## Current Code (short snippets with file:line refs - skip for brand-new files); " +
		"## Changes (the concrete edits, per file or concern); ## Performance Impact (expected runtime, memory, I/O, or token-usage impact - write Negligible. if there is none); " +
		"## Critical Files (backward-compat-sensitive code); ## Edge Cases; ## Verification (concrete end-to-end steps). " +
		"The 'title' argument must be a short human-readable phrase (60 chars max, no slashes) that names the plan file. If you need clarification, ASK - do not guess and do not request approval yet.",
	"auto": "You are now in Auto-Accept mode: tool approvals are bypassed. You may execute tools freely without waiting for per-call approval; with that autonomy comes a duty of care around destructive or irreversible actions. " +
		"DESTRUCTIVE-ACTION POLICY: treat as high-risk deleting or overwriting files or data, force operations (git push --force, git reset --hard), dropping or altering databases and cloud resources, mass or recursive removals (rm -rf), publishing externally (releases, comments, channel messages), and anything you cannot easily undo. " +
		"Before any high-risk action, STOP and confirm with the user in a normal message - state exactly what you will run and why - instead of relying on the (now-disabled) approval gate; proceed only once they agree, or when the task you were given already authorised it explicitly. " +
		"If no user is reachable (headless/unattended run), do NOT take a high-risk action on your own initiative: prefer the reversible path, narrow the scope, or stop and report what you would have done and why. " +
		"Low-risk, reversible work (reads, builds, tests, and edits within the working directory) proceeds normally - do not over-ask on routine steps. Never echo, print, or publish the value of a secret or environment variable. " +
		"The full tool set is available again (Write/Edit/Delete/Bash included); plan-only tools (RequestPlanApproval, AskUserQuestion) are disabled and will be rejected.",
	"auto-with-judge": "You are now in Auto+Judge mode: no human approves tool calls - an LLM judge decides every gated call against the user's latest request (config in judge.yaml). " +
		"Allow-listed commands still run for free; anything off-list is judged, and a rejection arrives as a rejection tool result explaining why - change the approach instead of retrying the same call. " +
		"Destructive or irreversible actions (rm -rf, force pushes, deleting data, publishing) are likely to be rejected unless the user's request clearly authorises them, and a failing judge also denies (fail closed) - if that happens, use an allow-listed command or stop and report. " +
		"The full tool set is available (Write/Edit/Delete/Bash included); plan-only tools (RequestPlanApproval, AskUserQuestion) are disabled and will be rejected.",
	"standard": "You are now in Standard mode: per-call tool approvals apply as configured - do not assume auto-acceptance; wait for each approval prompt (in agent mode, out-of-allow-list commands are rejected and must be reworked). " +
		"The full tool set is available again (Bash, Write/Edit/Delete included); plan-only tools (RequestPlanApproval, AskUserQuestion) are disabled and will be rejected. Check the BASH ALLOW-LIST in the current context reminder before proposing shell commands.",
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
			Hook:    agentdomain.HookPreSession,
			Trigger: ReminderTriggerOnce,
			Text:    defaultMemoryConsultReminderText,
		},
		{
			Name:     "memory-hygiene",
			Hook:     agentdomain.HookPreStream,
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
			Hook:     agentdomain.HookPreStream,
			Trigger:  ReminderTriggerInterval,
			Interval: defaultReminderInterval,
			When:     ReminderWhenTodosEmpty,
			Text:     defaultTodoReminderText,
		},
		{
			Name:     DefaultModeChangeReminderName,
			Hook:     agentdomain.HookPreStream,
			Trigger:  ReminderTriggerOnModeChange,
			Text:     DefaultModeChangeReminderText,
			Guidance: maps.Clone(defaultModeChangeGuidance),
		},
		{
			Name:      "user-intent-focus",
			Hook:      agentdomain.HookPreStream,
			Trigger:   ReminderTriggerOnceAfter,
			Threshold: defaultUserIntentFocusThreshold,
			Text:      defaultUserIntentFocusReminderText,
		},
	}
	reminders = append(reminders,
		ReminderConfig{
			Name:      "repeated-failure",
			Hook:      agentdomain.HookPostTool,
			Trigger:   ReminderTriggerOnRepeatedFailure,
			Threshold: defaultRepeatedFailureThreshold,
			Text:      defaultRepeatedFailureReminderText,
		},
		ReminderConfig{
			Name:      "todo-continuation",
			Hook:      agentdomain.HookPostStream,
			Trigger:   ReminderTriggerOnStalledTodos,
			Threshold: defaultStalledTodosThreshold,
			Text:      defaultTodoContinuationReminderText,
		},
		ReminderConfig{
			Name:    "truncation-continuation",
			Hook:    agentdomain.HookPostStream,
			Trigger: ReminderTriggerOnTruncation,
			Text:    defaultTruncationContinuationReminderText,
		},
	)
	reminders = append(reminders, MemoryReminders()...)
	return &RemindersConfig{
		Enabled:   true,
		Reminders: reminders,
	}
}

// LoadReminders reads reminders.yaml from disk. When the file is missing it
// returns the in-code defaults so callers can treat absence as "use defaults".
func LoadReminders(path string) (*RemindersConfig, error) {
	return configutils.LoadYAML(path, "reminders", DefaultRemindersConfig)
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
func (r RemindersConfig) MergeWithDefaults() *RemindersConfig {
	defaults := DefaultRemindersConfig()

	byName := make(map[string]int, len(defaults.Reminders))
	for i, def := range defaults.Reminders {
		byName[def.Name] = i
	}

	out := slices.Clone(defaults.Reminders)

	for _, supplied := range r.Reminders {
		if idx, ok := byName[supplied.Name]; ok {
			out[idx] = supplied
		} else {
			out = append(out, supplied)
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
	return configutils.SaveYAML(path, "reminders", cfg)
}

// effective returns the reminders with per-entry defaults applied: an empty
// Hook becomes pre_stream, an empty Trigger becomes always, and an interval
// trigger with a non-positive interval falls back to 4.
func (r RemindersConfig) effective() []ReminderConfig {
	out := make([]ReminderConfig, len(r.Reminders))
	for i, rc := range r.Reminders {
		if rc.Hook == "" {
			rc.Hook = agentdomain.HookPreStream
		}
		if rc.Trigger == "" {
			rc.Trigger = ReminderTriggerAlways
		}
		if rc.Trigger == ReminderTriggerInterval {
			rc.Interval = cmp.Or(rc.Interval, defaultReminderInterval)
		}
		if rc.Trigger == ReminderTriggerOnModeChange {
			guidance := make(map[string]string, len(defaultModeChangeGuidance))
			for k, v := range defaultModeChangeGuidance {
				guidance[k] = v
			}
			for k, v := range rc.Guidance {
				guidance[k] = v
			}
			rc.Guidance = guidance
		}
		out[i] = rc
	}
	return out
}

// RemindersDue implements agentdomain.SystemReminderProvider: it returns every
// reminder attached to q.Hook whose trigger fires. Multiple reminders on the
// same hook stack (all are returned). The interval trigger keys off
// q.SessionTurn (cumulative across the chat session) so it fires on every Nth
// conversational turn; turns_before_max keys off q.Turn/q.MaxTurns (the current
// run's loop budget). q.Fired is consulted only by the `once` trigger and is
// never written here - the caller marks names fired after injecting. A nil
// q.Fired is treated as "nothing fired yet".
func (r RemindersConfig) RemindersDue(q agentdomain.ReminderQuery) []agentdomain.SystemReminder {
	if !r.Enabled {
		return nil
	}
	var due []agentdomain.SystemReminder
	for _, rc := range r.effective() {
		if rc.Hook != q.Hook {
			continue
		}
		if !reminderWhenHolds(rc, q) {
			continue
		}
		if !reminderTriggerFires(rc, q) {
			continue
		}
		text := rc.Text
		switch rc.Trigger {
		case ReminderTriggerOnModeChange:
			text = resolveModeChangeText(rc, q)
		case ReminderTriggerOnRepeatedFailure:
			text = resolveRepeatedFailureText(rc, q)
		case ReminderTriggerOnStalledTodos:
			text = resolveStalledTodosText(rc, q)
		}
		due = append(due, agentdomain.SystemReminder{
			Name:               rc.Name,
			Text:               text,
			AppendToToolResult: rc.Trigger == ReminderTriggerOnRepeatedFailure,
		})
	}
	return due
}

// resolveModeChangeText substitutes the {prev_mode}/{new_mode}/{guidance}
// placeholders of an on_mode_change reminder from the query's mode transition.
// Guidance precedence: a user-edited guidance key on the reminder wins, then
// the per-mode adjustment instructions carried on the query (prompts.yaml
// agent.mode_adjustment_plan/_auto), then the built-in default for the mode.
func resolveModeChangeText(rc ReminderConfig, q agentdomain.ReminderQuery) string {
	key := q.Mode.ModeKey()
	guidance := rc.Guidance[key]
	if guidance == "" || guidance == defaultModeChangeGuidance[key] {
		if override := q.ModeGuidance[key]; override != "" {
			guidance = override
		}
	}
	if guidance == "" {
		guidance = "You are now in " + q.Mode.DisplayName() + " mode."
	}
	text := strings.ReplaceAll(rc.Text, "{prev_mode}", q.PrevMode.DisplayName())
	text = strings.ReplaceAll(text, "{new_mode}", q.Mode.DisplayName())
	return strings.ReplaceAll(text, "{guidance}", guidance)
}

// resolveRepeatedFailureText substitutes the {tool_name}/{count} placeholders
// of an on_repeated_failure reminder from the query's failure tracking.
func resolveRepeatedFailureText(rc ReminderConfig, q agentdomain.ReminderQuery) string {
	text := strings.ReplaceAll(rc.Text, "{tool_name}", q.FailedTool)
	return strings.ReplaceAll(text, "{count}", fmt.Sprintf("%d", q.RepeatedFailures))
}

// resolveStalledTodosText substitutes the {todo_list} placeholder of an
// on_stalled_todos reminder from the query's incomplete todo items.
func resolveStalledTodosText(rc ReminderConfig, q agentdomain.ReminderQuery) string {
	var items strings.Builder
	for _, t := range q.IncompleteTodos {
		fmt.Fprintf(&items, "- [%s] %s\n", t.Status, t.Content)
	}
	return strings.ReplaceAll(rc.Text, "{todo_list}", items.String())
}

// reminderWhenHolds evaluates the optional `when` state condition. An empty
// When always holds; an unknown value never does (fail-closed so a typo
// surfaces as a silent reminder rather than an unconditional one).
func reminderWhenHolds(rc ReminderConfig, q agentdomain.ReminderQuery) bool {
	switch rc.When {
	case "":
		return true
	case ReminderWhenTodosEmpty:
		return q.TodoCount == 0
	default:
		return false
	}
}

func reminderTriggerFires(rc ReminderConfig, q agentdomain.ReminderQuery) bool {
	switch rc.Trigger {
	case ReminderTriggerInterval:
		interval := cmp.Or(rc.Interval, defaultReminderInterval)
		return q.SessionTurn > 0 && q.SessionTurn%interval == 0
	case ReminderTriggerOnceAfter:
		return !q.Fired[rc.Name] && q.SessionTurn >= rc.Threshold
	case ReminderTriggerTurnsBeforeMax:
		return !q.Fired[rc.Name] && q.MaxTurns > 0 && rc.Threshold > 0 && (q.MaxTurns-q.Turn) <= rc.Threshold
	case ReminderTriggerOnce:
		return !q.Fired[rc.Name]
	case ReminderTriggerOnFailure:
		return q.ToolFailed
	case ReminderTriggerOnModeChange:
		return q.ModeChanged
	case ReminderTriggerAlways:
		return true
	case ReminderTriggerOnRepeatedFailure:
		threshold := cmp.Or(rc.Threshold, defaultRepeatedFailureThreshold)
		return q.RepeatedFailures >= threshold
	case ReminderTriggerOnTruncation:
		return q.FinishReason == "length"
	case ReminderTriggerOnStalledTodos:
		strikeCap := cmp.Or(rc.Threshold, defaultStalledTodosThreshold)
		return len(q.IncompleteTodos) > 0 && q.StalledStrikes < strikeCap
	default:
		return false
	}
}

// Validate checks each reminder against the pre-defined hook and trigger
// catalogs and the per-trigger requirements. It returns an error describing the
// first invalid entry. An empty Hook/Trigger is allowed (defaulted by
// effective); only non-empty values are checked against the catalog.
//
//nolint:gocyclo,cyclop // Validate is a flat switch of independent checks on the same loop; splitting would scatter closely related validation logic.
func (r RemindersConfig) Validate() error {
	for i, rc := range r.Reminders {
		switch {
		case rc.Name == "":
			return fmt.Errorf("reminders[%d]: name is required", i)
		case rc.Text == "":
			return fmt.Errorf("reminders[%d] (%s): text is required", i, rc.Name)
		case rc.Hook != "" && !rc.Hook.Valid():
			return fmt.Errorf("reminders[%d] (%s): unknown hook %q (valid: %v)", i, rc.Name, rc.Hook, agentdomain.HookPoints)
		case rc.Trigger != "" && !rc.Trigger.Valid():
			return fmt.Errorf("reminders[%d] (%s): unknown trigger %q (valid: %v)", i, rc.Name, rc.Trigger, ReminderTriggers)
		case rc.Trigger == ReminderTriggerOnFailure && rc.Hook != agentdomain.HookPostTool:
			return fmt.Errorf("reminders[%d] (%s): trigger on_failure requires hook %s", i, rc.Name, agentdomain.HookPostTool)
		case rc.Trigger == ReminderTriggerTurnsBeforeMax && rc.Threshold <= 0:
			return fmt.Errorf("reminders[%d] (%s): trigger turns_before_max requires threshold > 0", i, rc.Name)
		case rc.Trigger == ReminderTriggerOnceAfter && rc.Threshold <= 0:
			return fmt.Errorf("reminders[%d] (%s): trigger once_after requires threshold > 0", i, rc.Name)
		case rc.Trigger == ReminderTriggerOnModeChange && rc.Hook != "" && rc.Hook != agentdomain.HookPreStream:
			return fmt.Errorf("reminders[%d] (%s): trigger on_mode_change requires hook %s", i, rc.Name, agentdomain.HookPreStream)
		case rc.Trigger == ReminderTriggerOnRepeatedFailure && rc.Hook != "" && rc.Hook != agentdomain.HookPostTool:
			return fmt.Errorf("reminders[%d] (%s): trigger on_repeated_failure requires hook %s", i, rc.Name, agentdomain.HookPostTool)
		case rc.Trigger == ReminderTriggerOnRepeatedFailure && rc.Threshold <= 0:
			return fmt.Errorf("reminders[%d] (%s): trigger on_repeated_failure requires threshold > 0", i, rc.Name)
		case rc.Trigger == ReminderTriggerOnTruncation && rc.Hook != "" && rc.Hook != agentdomain.HookPostStream:
			return fmt.Errorf("reminders[%d] (%s): trigger on_truncation requires hook %s", i, rc.Name, agentdomain.HookPostStream)
		case rc.Trigger == ReminderTriggerOnStalledTodos && rc.Hook != "" && rc.Hook != agentdomain.HookPostStream:
			return fmt.Errorf("reminders[%d] (%s): trigger on_stalled_todos requires hook %s", i, rc.Name, agentdomain.HookPostStream)
		case rc.Interval < 0:
			return fmt.Errorf("reminders[%d] (%s): interval must be >= 0", i, rc.Name)
		}
		for key := range rc.Guidance {
			if _, ok := agentdomain.ParseAgentMode(key); !ok {
				return fmt.Errorf("reminders[%d] (%s): unknown guidance mode key %q (valid: standard, plan, auto, auto-with-judge, readonly)", i, rc.Name, key)
			}
		}
	}
	return nil
}
