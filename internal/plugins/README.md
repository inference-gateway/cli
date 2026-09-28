# plugins

**What** - the plugins capability: installing Claude Code-format plugins (skills plus an always-on `AGENTS.md` ruleset) from GitHub.
**Why** - operators want to share agent behaviour as content, and the CLI must do that without ever executing third-party code.
**How** - `infer plugins install owner/repo` fetches and unpacks the plugin, then its skills join the
skill catalog and its instructions join the system prompt.

## What it owns

- `source.go`, `installer.go` - resolving a GitHub source and installing, updating or removing a plugin on disk.
- `manifest.go` - the plugin manifest: name, version and what the plugin contributes.
- `instructions.go` - loading the plugin's always-on ruleset and exposing it to the prompt builder.
- `hooks.go` - mapping the plugin's skills into the skills catalog and its instructions into the agent brief.

## Why it is separate

A plugin is untrusted content with two different shapes (skills, instructions) and a lifecycle
(install, enable, disable, update, remove). That is its own domain, and keeping it apart makes the
"content only, code is never executed" guarantee easy to audit.

## How it plugs in

- Manage plugins with `infer plugins install|list|disable|update|remove`; installed plugins live under the userspace config directory.
- Skills contributed by a plugin flow through `internal/skills`; instructions flow through the agent's prompt assembly.
- A disabled plugin stops contributing both, without deleting the download.

## Related

- [Plugins](../../docs/plugins.md)
- [Agent Skills](../../docs/skills.md)
- [Configuration Reference](../../docs/configuration-reference.md)
