# Agent Skills

Skills are instruction folders the agent loads on demand: one directory with a `SKILL.md` per skill, discovered at startup, listed in the system
prompt's `AVAILABLE SKILLS` block, and activated by name with the `Skill` tool. This example ships one such skill in the open-standard location:

```text
.agents/skills/commit-style/SKILL.md
```

`.agents/skills/` is the emerging cross-tool convention (Claude Code, Gemini CLI, Codex CLI), so a skill authored there works in `infer`
without moving it. Two other local locations are scanned the same way, in precedence order: `.infer/skills/<name>/` (`scope: project`) and
`~/.infer/skills/<name>/` (`scope: user`). Skills contributed by installed plugins arrive as `scope: plugin` - see [Plugins](../plugins/).

The `name` (lowercase letters, digits, hyphens, equal to the directory name) and the `description` (the routing signal that tells the model
when to activate) live in YAML frontmatter. The body below it is only loaded when the skill is used. The model is a
[tokenless](https://github.com/inference-gateway/tokenless) mock scripted in [`scenarios.yaml`](scenarios.yaml), so the example runs offline with no
API key and no gateway. You need [Task](https://taskfile.dev) to run the scenarios.

See [Skills](../../docs/skills.md) for the complete reference.

## Layout

```text
skills/
├── Taskfile.yml                          # one task per scenario, with the mock model's environment
├── scenarios.yaml                        # what the mock model says, per prompt
└── .agents/skills/
    └── commit-style/
        └── SKILL.md                      # frontmatter: name + description. The body follows it
```

## Run it

| Task | What happens |
| --- | --- |
| `task list` | `infer skills list` discovers the skill and reports its `agents` scope |
| `task load` | The mock model calls the `Skill` tool with `name: commit-style`, and the tool result carries the SKILL.md body |
| `task disabled` | With `INFER_AGENT_SKILLS_ENABLED=false` the list still runs (with a note), but the `Skill` tool is refused |
| `task chat` | Opens `infer chat` with the mock model. Type `/commit-style` to load the skill yourself |

The tasks use the `infer` on your `PATH`. To use a local build instead, run `task build` in the repository root and pass it in, for example
`task load INFER=../../infer`. The list tasks pin progressive discovery off (`INFER_AGENT_SKILLS_DISCOVERY_ENABLED=false`), so `Discovered
Skills` shows only the skills in this directory - catalog skills appear through the explicit `search` and `install` commands below.

### What to expect

`task list` - the skill is discovered from `.agents/skills` with scope `agents`:

```text
Discovered Skills (1)

  NAME           SCOPE    DESCRIPTION                                                     PATH
  commit-style   agents   House rules for commit messages in this repository. Use w ...   .../examples/skills/.agents/skills/commit-style/SKILL.md
```

`task load` - the tool result carries the skill body:

```text
{"content":"{\"tool_name\":\"Skill\",...,\"data\":{\"name\":\"commit-style\",\"content\":\"# Commit style ...\"}}","failed":false,"role":"tool", ...}
```

`task disabled` - the `Skill` tool is off with the feature, so the scripted call is refused:

```text
{"content":"tool Skill is not enabled","failed":true,"role":"tool", ...}
```

## The catalog, on a real network

Local skills are only half the picture. The CLI also has a centralized skills catalog published by
[inference-gateway/skills](https://github.com/inference-gateway/skills), which needs network access, so these are not part of the offline tasks
above:

```bash
# Fuzzy-name search over the fetched catalog index (omit the term to browse)
infer skills search rust
infer skills search "pull request" --limit 20

# Install by catalog name, org/skill pair, or full GitHub tree URL
infer skills install skill-creator
infer skills install acme/internal-comms
infer skills install https://github.com/anthropics/skills/tree/main/skills/pdf

# Defaults to .infer/skills/ in the current project. --user installs to ~/.infer/skills/
infer skills install skill-creator --user

# Remove the downloaded folder (no confirmation prompt)
infer skills uninstall pdf
```

`install` runs the same frontmatter validator the startup scan uses, so what installs is what loads. Requests are unauthenticated (GitHub allows
60 per hour per IP) and go up to 5,000 per hour with `GITHUB_TOKEN` set.

## The switch

Skills are **enabled by default**. Turn them off - which skips the startup scan entirely, so there is zero token cost:

```yaml
# .infer/config.yaml
agent:
  skills:
    enabled: false
```

```bash
INFER_AGENT_SKILLS_ENABLED=false infer chat
```

`task disabled` runs with the switch off: `infer skills list` still works (it always does, so you can verify discovery) and its note says the
feature is disabled, while the mock model's `Skill` call comes back refused.

## Without Task

The Taskfile only sets four environment variables. Set them yourself to run `infer` directly from this directory:

```bash
export INFER_GATEWAY_MOCK=true
export INFER_GATEWAY_MOCK_SCENARIOS="$PWD/scenarios.yaml"
export INFER_AGENT_MODEL=mock/openai/gpt-4o
export INFER_STORAGE_ENABLED=false
export INFER_AGENT_SKILLS_DISCOVERY_ENABLED=false

infer skills list
infer headless --no-save "load the commit-style skill" | grep '"role":"tool"'
```

## Use a real model

Drop `INFER_GATEWAY_MOCK`, `INFER_GATEWAY_MOCK_SCENARIOS` and `INFER_AGENT_MODEL` to talk to a real model through the gateway. Nothing else
changes: a capable model picks `commit-style` up from the `AVAILABLE SKILLS` block on its own when you ask it to commit something.
