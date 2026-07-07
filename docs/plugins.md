# Plugins

`infer plugins` installs **Claude Code-format plugins** — content packages that
bundle Agent Skills and an always-on instruction ruleset — and surfaces them
through infer's native primitives. The Claude Code plugin layout is the closest
thing to a de-facto packaging standard across agent CLIs, so plugins published
for that ecosystem (e.g. [ponytail](https://github.com/DietrichGebert/ponytail))
install into infer unchanged.

```sh
infer plugins install DietrichGebert/ponytail
```

## What gets installed

A plugin repo is mapped onto native infer features — **content only**:

| Plugin component | infer feature |
| --- | --- |
| `skills/<name>/SKILL.md` | Native skills system, new `plugin` scope (autocomplete, `/name` invocation, `infer skills list`) |
| `AGENTS.md` (repo root) | Injected into the system prompt as a labeled `PLUGIN INSTRUCTIONS (<name>)` section while the plugin is enabled |
| `hooks.yaml` (repo root) | Native Infer command hooks (disabled by default; enable with `infer plugins enable-hooks <name>`) |
| `.claude-plugin/plugin.json` | Name/version/description metadata (optional — falls back to the repo name) |
| `hooks/`, `commands/`, `agents/` | **Detected and reported, never executed or installed** |

Only that mapped subset is downloaded. infer deliberately does not adopt
executable plugin models (e.g. OpenCode's JavaScript plugins): a plugin can
add instructions and skills, but it can never run code on your machine.

Because a plugin's `AGENTS.md` becomes always-on prompt content, `install`
prints a summary of what the plugin provides and asks for confirmation
(`--yes` skips it; non-interactive stdin requires `--yes`).

## Commands

```sh
infer plugins install <owner/repo | github-url | local-path> [--ref <ref>] [--yes] [--overwrite]
infer plugins list [--format text|json]
infer plugins update [<name>]        # re-fetch from the install source
infer plugins enable <name>
infer plugins disable <name>         # skills unloaded, instructions removed
infer plugins enable-hooks <name>    # opt in to plugin command hooks (off by default)
infer plugins disable-hooks <name>   # opt out of plugin command hooks
infer plugins remove <name>
```

Sources: `owner/repo` (optionally `owner/repo@ref`), a `github.com` URL
(optionally `/tree/<ref>`), or a local directory (`./my-plugin`) for plugin
development. Without a ref, `HEAD` (the default branch) is used. Downloads go
through the GitHub tree + raw APIs — no `git` binary needed; set
`GITHUB_TOKEN`/`GH_TOKEN` for higher rate limits or private repos.

## Storage and layering

Plugins are **userspace-only**: content lives under `~/.infer/plugins/<name>/`
and the registry is `~/.infer/plugins.yaml`, created on first install (never
seeded or overwritten by `infer init`).

- **Skills precedence**: project (`.infer/skills`) → `.agents/skills` →
  user (`~/.infer/skills`) → plugins. A local skill with the same name always
  overrides a plugin's.
- **Instructions merge, never replace** (matching the AGENTS.md standard):
  base prompt → `custom_instructions` → your project `AGENTS.md` → each
  enabled plugin's `AGENTS.md`, in registry order.
- Instruction files are read verbatim (no environment-variable expansion) and
  capped at `max_instructions_lines` (default 399, per the AGENTS.md standard)
  with a `max_instructions_chars` backstop (default 8000); truncation is
  always explicitly marked.
- In Claude Code mode, plugin rulesets ride `--append-system-prompt`; your
  project `AGENTS.md` is not appended there because the `claude` CLI reads it
  natively.

## Configuration

`~/.infer/plugins.yaml`:

```yaml
---
enabled: true                  # master switch (INFER_PLUGINS_ENABLED)
# dir: /custom/path            # storage override (INFER_PLUGINS_DIR)
# max_instructions_lines: 399
# max_instructions_chars: 8000
plugins:
  - name: ponytail
    source: DietrichGebert/ponytail
    version: 4.8.4
    enabled: true
```

Related toggles:

- `agent.skills.enabled: false` stops plugin skills from loading (plugin
  instructions still inject; disable the plugin or set
  `INFER_PLUGINS_ENABLED=false` to remove everything).
- `agent.agents_md.enabled` / `max_lines` / `max_chars` control the native
  injection of your own project `AGENTS.md` (`INFER_AGENT_AGENTS_MD_*`).

## Verifying what the model sees

```sh
infer skills list                          # plugin skills appear with scope "plugin"
infer debug agent system_prompt | grep -A3 "PLUGIN INSTRUCTIONS"
```
