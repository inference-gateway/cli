# Reminders & Command Hooks

[← Back to README](../README.md)

**What** - two lightweight extension points that fire at fixed agent-loop hook points: reminders
inject a system-reminder text block, command hooks run a shell command.
**Why** - they let you enforce project conventions (format on session end, nudge todo hygiene) without forking the agent or editing the system prompt.
**How** - declare reminders in `reminders.yaml` with a trigger, and command hooks in `hooks.yaml`
with an executable that must also pass the per-mode bash allow-list.

- **Reminders** (`reminders.yaml`) ship **enabled** with nine built-in defaults. Hook points, triggers
  and the defaults are listed in the [configuration reference](configuration-reference.md#system-reminders-remindersyaml);
  the memory reminders are auto-pruned when memory is off.
- **Command Hooks** (`hooks.yaml`) run a shell command at the same hook points - the executable sibling of
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
