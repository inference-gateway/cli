# skills

**What** - the Agent Skills capability: discovering `SKILL.md` folders, searching the remote catalog, and installing or removing skills.
**Why** - skills are instructions the agent loads only when relevant, so they need discovery, precedence and a safe
install path rather than a hardcoded prompt.
**How** - `skills.go` implements the agent's skills port, `catalog.go` fetches the remote registry index, and `infer skills` drives both.

## How it plugs in

- Scanning order, highest precedence first: `.infer/skills/`, then `.agents/skills/`, then `~/.infer/skills/`,
  then the enabled plugins' skills.
- Gated by `agent.skills.enabled` (on by default); `infer skills list` still discovers skills when disabled.
- The tools registry and the prompt builder only consume a list of names and descriptions.

## Related

- [Agent Skills](../../docs/skills.md)
- [Plugins](../../docs/plugins.md)
