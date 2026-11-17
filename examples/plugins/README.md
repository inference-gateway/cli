# Plugins

A plugin is a content package: skills, an always-on instruction ruleset, and command hooks you opt into. The format is the Claude Code plugin
layout (`.claude-plugin/plugin.json`, `skills/`, root `AGENTS.md`, `hooks/`, `commands/`), so plugins published for that ecosystem install into
`infer` unchanged - including from a local directory, which is how you develop one. This example installs `my-plugin/` from disk.

Plugins are **userspace-only**: content lives under `~/.infer/plugins/<name>/` and the registry is `~/.infer/plugins.yaml`, created on the first
install. To keep the example out of your real home, every task runs under a demo HOME (`examples/plugins/.home`) the way `examples/web-terminal`
does. The mock model is not needed here - these are registry commands, no gateway involved. You need [Task](https://taskfile.dev).

What the demo plugin ships, and which infer feature each part maps to:

| Plugin component | infer feature |
| --- | --- |
| `skills/repo-hygiene/SKILL.md` | The native skills system, discovered as `scope: plugin` |
| `AGENTS.md` (root) | Injected as a labeled `PLUGIN INSTRUCTIONS (demo-hygiene)` section of every system prompt |
| `hooks.yaml` (root) | infer command hooks, installed **disabled** until you opt in |
| `.claude-plugin/plugin.json` | Name, version and description metadata |
| `commands/hello.md` | **Detected and reported, never executed or installed** - infer does not run plugin code |

See [Plugins](../../docs/plugins.md) for the full reference.

## Layout

```text
plugins/
├── Taskfile.yml                      # every command runs with HOME set to the demo HOME
├── my-plugin/
│   ├── .claude-plugin/plugin.json    # manifest: name "demo-hygiene", version, description
│   ├── AGENTS.md                     # always-on instructions while the plugin is enabled
│   ├── hooks.yaml                    # one post_session hook, installed disabled
│   ├── commands/hello.md             # counted and reported, never run
│   └── skills/
│       └── repo-hygiene/
│           └── SKILL.md              # frontmatter name + description, like any skill
└── .home/                            # demo HOME, created by install, removed by task clean
```

## Run it

| Task | What happens |
| --- | --- |
| `task install` | `infer plugins install ./my-plugin --yes` - prints the summary, copies the mapped subset into the demo HOME |
| `task list` | `infer plugins list` reads the demo HOME's registry |
| `task skills` | `infer skills list` shows `repo-hygiene` with scope `plugin` |
| `task prompt` | `infer debug agent system_prompt` shows `PLUGIN INSTRUCTIONS (demo-hygiene)` in the prompt |
| `task enable-hooks` | `hooks_enabled` is absent while disabled, present as `true` after `infer plugins enable-hooks demo-hygiene` |
| `task disable` | `infer plugins disable demo-hygiene` - skill gone from `infer skills list`, instructions gone from the prompt |
| `task enable` | `infer plugins enable demo-hygiene` - everything back |
| `task update` | `infer plugins update demo-hygiene` re-copies from the recorded source (the local directory) |
| `task remove` | `infer plugins remove demo-hygiene` deletes the folder and the registry entry |
| `task all` | Runs the lifecycle above, end to end, ending with the plugin removed |
| `task clean` | Removes the demo HOME |

Pass `INFER=../../infer` after `task build` in the repository root to use a local build, for example `task install INFER=../../infer`.

### What to expect

`task install` - the summary names what activates before the `--yes` confirmation runs, and reports the detected-but-ignored directory:

```text
Plugin: demo-hygiene
Version: 0.1.0
Description: A local demo plugin: one skill, an always-on instruction ruleset, and a hooks.yaml
Source: .../examples/plugins/my-plugin
Skills (1): repo-hygiene
Instructions: AGENTS.md (348 chars) - injected into EVERY system prompt while enabled
Hooks (1): demo-hook (post_session)
  hooks are disabled by default - use `infer plugins enable-hooks <name>` to opt in
detected but ignored: commands/ (1 files) - infer does not execute plugin code

✓ Installed plugin demo-hygiene to .../examples/plugins/.home/.infer/plugins/demo-hygiene
```

`task skills` - the plugin contributes to the normal skills scan:

```text
Discovered Skills (1)

  NAME            SCOPE    DESCRIPTION ... PATH
  repo-hygiene    plugin   Checks a repository for stray scratch files ... .../.infer/plugins/demo-hygiene/skills/repo-hygiene/SKILL.md
```

`task enable-hooks` - the registry flag appears only after the opt-in:

```text
no hooks_enabled flag in the registry: plugin hooks are disabled
✓ Plugin hooks for demo-hygiene enabled
hooks_enabled: true
```

## Running the plugin's hooks for real

`enable-hooks` only flips the per-plugin flag. Three more switches stand between a plugin hook and your shell, on purpose: the master
`hooks.enabled` (hooks.yaml at the project or user level, default off), the bash allow-list, and the same points that gate your own hooks. See
[Reminders & Command Hooks](../hooks/) for a hook that actually runs and the skip behavior when it is not allow-listed.

The `commands/` directory never participates: its files are counted and listed under "detected but ignored", and nothing in
`hooks/`, `commands/` or `agents/` is installed.

## Without Task

Set `HOME` yourself, and stay in this directory:

```bash
export HOME="$PWD/.home"

infer plugins install ./my-plugin --yes
infer plugins list
infer skills list
infer debug agent system_prompt | grep -A3 PLUGIN INSTRUCTIONS
infer plugins enable-hooks demo-hygiene
infer plugins remove demo-hygiene
```

`task clean` removes `.home` when you are done. Installing from a real registry works the same way: `infer plugins install owner/repo` fetches
the same mapped subset over the GitHub API - no `git` binary needed - and a local directory is exactly one alternative source.
