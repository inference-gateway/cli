# skills

**What** - the Agent Skills capability: discovering `SKILL.md` folders, cataloguing them, and installing or removing them from GitHub.
**Why** - skills are reusable instructions the agent loads only when relevant, so it needs discovery,
precedence and a safe install path rather than a hardcoded prompt.
**How** - the agent reaches them through a service port; `infer skills` and the `/skills` shortcut drive the same catalog.

## What it owns

- `catalog.go` - scanning the three skill locations and resolving name collisions.
- `skills.go` - the in-memory list, precedence rules and lookup used by the agent.
- `install.go` - `infer skills install`, fetching a `SKILL.md` folder from GitHub by name, `org/name`, or tree URL.
- `builtins.go` - the skills shipped with the CLI.

## Why it is separate

Skills are content, not code. Keeping the format, the precedence rules and the fetch logic together
means the tools registry and the agent prompt builder only consume a list of names and descriptions.

## How it plugs in

- Scanning order, highest precedence first: `.infer/skills/`, then the open `.agents/skills/` standard, then `~/.infer/skills/`.
- Gated by `agent.skills.enabled` (on by default); `infer skills list` still discovers skills when disabled.
- `GITHUB_TOKEN` or `GH_TOKEN` raises the GitHub rate limit and reaches private repositories.

## Related

- [Agent Skills](../../docs/skills.md)
- [Plugins](../../docs/plugins.md)
- [Commands Reference](../../docs/commands-reference.md)
