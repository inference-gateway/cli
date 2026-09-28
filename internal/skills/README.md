# skills

**What** - the Agent Skills capability: discovering `SKILL.md` folders, searching the remote catalog, and installing or removing skills.
**Why** - skills are instructions the agent loads only when relevant, so they need discovery, precedence and a safe
install path rather than a hardcoded prompt.
**How** - `skills.go` implements the agent's skills port, `catalog.go` fetches the remote registry index, and `infer skills` drives both.

## How it plugs in

- Scanning order, highest precedence first: `.infer/skills/`, then `.agents/skills/`, then `~/.infer/skills/`,
  then the enabled plugins' skills.
- `infer skills list` still discovers skills when the feature is disabled.
- The agent's prompt builder reads skills through the skills port in `internal/agent/domain`.

## Related

- [Agent Skills](../../docs/skills.md)
- [Plugins](../../docs/plugins.md)
