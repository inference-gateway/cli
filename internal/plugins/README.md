# plugins

**What** - the plugins capability: installing Claude Code-format plugins (skills plus an always-on `AGENTS.md` ruleset) from GitHub or a local path.
**Why** - operators want to share agent behaviour as a package, with a lifecycle (install, enable, disable, update, remove) that is easy to audit.
**How** - `infer plugins install` fetches and unpacks a plugin, then its skills join the skill catalog and its instructions join the system prompt.

## How it plugs in

- Skills contributed by a plugin flow through `internal/skills` as the `plugin` scope.
- The container feeds the enabled plugins' instructions into prompt assembly.
- A plugin may ship command hooks (`hooks.yaml`). They stay off until `infer plugins enable-hooks <name>` and then
  run as agent command hooks - the only way a plugin executes code.
- A disabled plugin stops contributing all three, without deleting the download.

## Related

- [Plugins](../../docs/plugins.md)
- [Agent Skills](../../docs/skills.md)
- [Reminders & Command Hooks](../../docs/hooks.md)
