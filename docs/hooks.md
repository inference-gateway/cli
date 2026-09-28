# Reminders & Command Hooks

[← Back to README](../README.md)

**What** - two lightweight extension points that fire at fixed agent-loop hook points: reminders
inject a system-reminder text block, command hooks run a shell command.
**Why** - they let you enforce project conventions (format on session end, nudge todo hygiene) without forking the agent or editing the system prompt.
**How** - declare reminders in `reminders.yaml` with a trigger, and command hooks in `hooks.yaml`
with an executable that must also pass the per-mode bash allow-list.

Two lightweight extension points fire at fixed **agent-loop hook points** - `pre_session`,
`pre_stream`, `post_stream`, `pre_tool`, `post_tool`, `pre_queue_drain`, `post_queue_drain`, and
`post_session`:

- **Reminders** (`reminders.yaml`) inject a `<system-reminder>` text block at a hook point, gated by a
  trigger (`always`, `interval`, `once_after`, `turns_before_max`, `once`, `on_failure`,
  `on_mode_change`, `on_repeated_failure`, `on_truncation`, `on_stalled_todos`, or
  `on_empty_response`; see the [configuration reference](configuration-reference.md#system-reminders-remindersyaml) for the
  full table). Reminders ship **enabled** with nine defaults: `todo-hygiene` (nudges the agent to
  keep a todo list), `mode-change-reminder`, `user-intent-focus`, `repeated-failure`,
  `todo-continuation`, `truncation-continuation`, `empty-response-continuation`, and the memory
  reminders `memory-consult` (points it at the memory index) and `memory-hygiene` (one nudge after
  25 turns to save what a future session would otherwise miss); the memory ones are auto-pruned
  when memory is off.
- **Command Hooks** (`hooks.yaml`) run a shell command at a hook point - the executable sibling of
  reminders. They are **off by default**; each command still faces the per-mode bash allow-list when
  the agent runs it, so allow-list the command and set `enabled: true` to turn hooks on.

```yaml
# .infer/hooks.yaml
enabled: true
hooks:
  - name: gofmt
    hook: post_session
    command: "gofmt -w ."
    timeout: 30   # seconds; 0 -> default 30
```

## Related

- [Configuration Reference](configuration-reference.md#system-reminders-remindersyaml) - every reminder trigger and option
- [Persistent Memory](memory.md) - the memory reminders that ship enabled
- [Commands Reference](commands-reference.md#global-flags) - `--reminders-file` and the other global flags
