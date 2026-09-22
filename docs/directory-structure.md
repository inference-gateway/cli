# Directory Structure

[← Back to README](../README.md)

This page is a map of every file and subdirectory the `infer` CLI reads or
writes. It complements [Configuration Reference](configuration-reference.md),
which documents what each *option* does - this page documents where each
*file* lives and why it exists.

## Table of Contents

- [The Two Layers](#the-two-layers)
- [At a Glance](#at-a-glance)
- [Userspace Files Seeded by `infer init`](#userspace-files-seeded-by-infer-init)
- [File Formats](#file-formats)
- [Created at Runtime](#created-at-runtime)
- [What to Commit, What to Ignore](#what-to-commit-what-to-ignore)

---

## The Two Layers

The CLI keeps everything in two locations:

- **Userspace layer** - `~/.infer/`, in your home directory. The default
  home of all configuration *and* all state: conversations, logs, history,
  tmp, artifacts, exports. This is the only location the CLI writes to by
  default.
- **Project layer** - `.infer/`, sitting next to your code. An *optional*
  config override layer that exists only if you create it. The CLI never
  writes to a project `.infer/` on its own; the only way files appear there
  is a config write you explicitly request with
  `infer config set --project ...` (or hand-editing).

Run `infer init` to seed the userspace baseline. Project values override
userspace values: defaults < `~/.infer/<file>` < `./.infer/<file>` (project
always wins). Note that list-valued keys (e.g. allowlists) in a project
override *replace* the userspace value wholesale - viper's `MergeInConfig`
deep-merges maps but substitutes slices. See
[Configuration Layers](configuration-reference.md#configuration-layers)
for the full precedence rules.

---

## At a Glance

```text
~/.infer/                 # userspace layer - the default and only written location
├── config.yaml           # main configuration
├── projects.yaml         # desktop sidebar projects and groups
├── desktop.yaml          # desktop app settings
├── auth.yaml             # provider API key fallback, mode 0600
├── prompts.yaml          # LLM system prompts (agent, git, conversation, tools, ...)
├── keybindings.yaml      # chat UI keyboard shortcuts
├── channels.yaml         # remote messaging channels (Telegram, ...)
├── computer_use.yaml     # computer-use / vision settings
├── browser_use.yaml      # browser automation (Playwright) settings
├── agents.yaml           # A2A agent registry
├── mcp.yaml              # MCP server registry
├── shortcuts/            # /-prefixed chat shortcuts (built-in + custom)
│   ├── git.yaml
│   ├── scm.yaml
│   ├── mcp.yaml
│   ├── shells.yaml
│   ├── export.yaml
│   └── a2a.yaml
├── skills/               # Agent Skills - SKILL.md folders, see docs/skills.md
├── schedules/            # cron-driven scheduled jobs (one YAML per job)
├── plans/                # plan-mode plans saved by RequestPlanApproval (one .md per plan)
├── insights/             # /insights session reports (one .md per run); survives /reset
├── logs/                 # CLI + gateway logs (app/debug/daemon/gateway <date>.log)
├── telemetry/            # usage stats backing `infer stats` (see docs/telemetry.md)
├── run/                  # daemon pid/lock files
├── tmp/                  # userspace scratch: agent-readable/writable, wiped by /reset
│   ├── tts/              # generated speech WAVs (text_to_speech.output_dir default)
│   ├── music/            # generated music MP3s (text_to_music.output_dir default)
│   ├── sfx/              # generated sound-effect WAVs (text_to_sfx.output_dir default)
│   ├── voice/            # retained inbound voice recordings (speech_to_text.recordings_dir default)
│   └── media/            # retained inbound Telegram media (channels.telegram.media.dir default)
├── bin/                  # downloaded gateway binary, one shared copy per machine
├── conversations.db      # shared SQLite conversation store (type: sqlite)
└── projects/             # per-project runtime state, grouped by project
    └── <project-slug>/
        ├── conversations/  # JSONL conversation stores (type: jsonl)
        ├── history/        # chat input history (one entry per line)
        ├── backups/        # file-write tool backups
        ├── tmp/            # scratch space (streamed writes, dynamic skills, ...)
        ├── artifacts/      # agent deliverables (images, downloads, ...)
        └── exports/        # `infer export` chat markdown exports

.infer/                   # OPTIONAL project layer - config overrides only,
│                         # created by you / `infer config set --project`
│                         # (the CLI never writes here on its own)
├── config.yaml           # sparse override of ~/.infer/config.yaml
├── mcp.yaml              # project MCP servers (project-then-home lookup)
├── keybindings.yaml      # project keybindings (project-then-home lookup)
├── shortcuts/            # project shortcuts, overlaid by name onto ~/.infer/shortcuts/
└── skills/               # project skills, still discovered when present

.agents/                  # open-standard project layer (cross-tool skills)
└── skills/               # Agent Skills - SKILL.md folders (read-only discovery)
    └── <name>/SKILL.md   # e.g. .agents/skills/pdf/SKILL.md
```

---

## Userspace Files Seeded by `infer init`

These are the files `infer init` writes once and then leaves to you. They
all live in `~/.infer/` - init never writes into a project directory. If a
project wants to override a config file it commits its own sparse
`.infer/<file>`, created with `infer config set --project` (see
[The Two Layers](#the-two-layers)).

- **`config.yaml`** - gateway, tools, storage, agent, chat, web and pricing
  settings. Edit by hand or via `infer config ...`. Full option-by-option
  reference: [Configuration Reference](configuration-reference.md).
- **`prompts.yaml`** - system prompts the LLM sees (agent, git,
  conversation, init, tools). Tool descriptions live under
  `tools.<ToolName>.description`.
- **`keybindings.yaml`** - keyboard shortcuts for the chat TUI. Edit via
  `infer keybindings set/disable/reset` or by hand.
- **`channels.yaml`** - remote messaging transports (Telegram, ...) and
  per-channel allowlists. See [Channels](channels.md). On first init, a
  legacy `channels:` block in `config.yaml` is auto-migrated here.
- **`computer_use.yaml`** - computer-use / vision tool settings.
  Auto-migrated from `config.yaml` on first init if the legacy block
  exists.
- **`browser_use.yaml`** - browser automation tool settings (Playwright
  browser channel / CDP endpoint, per-tool enable flags, rate limiting).
- **`agents.yaml`** - A2A agent registry (URLs, models, env vars). Manage
  via `infer agents add/remove/list`. See
  [A2A Agents](agents-configuration.md).
- **`mcp.yaml`** - MCP server registry and liveness probe settings. Manage
  via `infer mcp ...` or by hand. See [MCP Integration](mcp-integration.md).
- **`shortcuts/*.yaml`** - `/git`, `/scm`, `/mcp`, `/shells`, `/export`, `/env`,
  `/agents`, `/skills` shortcuts plus any you add. Drop new YAML files into
  `shortcuts/`. A project `./.infer/shortcuts/` is overlaid on top by shortcut
  name, so it adds to (or replaces individual entries of) the userspace set
  rather than hiding it. See [Shortcuts Guide](shortcuts-guide.md).
- **`skills/`** - Agent Skills directory. Drop a `SKILL.md` folder here (or
  into the cross-tool `.agents/skills/` open standard) to extend the agent.
  See [Skills](skills.md).

The split into separate YAML files (rather than one giant `config.yaml`) is
deliberate: each concern has its own file so changes stay focused and
reviews stay readable.

### File Formats

A simple rule keeps `~/.infer/` consistent:

- **Hand-edited files are YAML** - `config.yaml`, `agents.yaml`, `auth.yaml`
  and everything else a user is expected to open in an editor. `auth.yaml`
  (mode 0600) holds the provider API key fallback.
- **State a user may inspect is YAML too** - `projects.yaml` (sidebar
  projects, groups and per-project path overrides) and `desktop.yaml` (app
  settings). Nobody hand-writes these, but people do open them to see why a
  chat landed in the wrong project, so the format rule follows what a human
  might read rather than who typed it. Both are **owned by the desktop
  repo**, which writes them; the CLI only carves `projects.yaml` out of the
  sandbox so the agent can edit it, and preserves it across `/reset`. They
  replace `projects.json` and `desktop.json` outright, with no compatibility
  read - the old files hold regenerable sidebar state, so the app rebuilds
  it rather than carrying two formats. See inference-gateway/desktop#283.
- **Opaque caches and cursors stay JSON** - `skills/catalog.json`
  (downloaded skill index), `schedules/github-artifacts-state.json` (GitHub
  artifact poller cursor) and `session_groups.json` (session group index).
  These are written and read by the CLI, carry no decision a user would
  want to review, and converting them would churn on-disk state for no
  gain. `.claude-plugin/plugin.json` follows an external spec and does not
  change.

---

## Created at Runtime

These are written by the CLI as you use it - `infer init` does **not**
create them. They are runtime output, not configuration, so they default to
`~/.infer/projects/<project-slug>/` (grouped by project) and never land in
the project-local `.infer/`.

- **`~/.infer/conversations.db`** *(userspace)* - shared SQLite conversation store, active
  when `storage.type: sqlite`. See
  [Conversation Storage](conversation-storage.md).
- **`~/.infer/projects/<project-slug>/conversations/*.jsonl`** *(userspace)* -
  active when `storage.type: jsonl`. One file per conversation.
- **`~/.infer/logs/`** *(userspace)* - debug and error logs (CLI and gateway).
  Path configurable via `logging.dir` / `INFER_LOGGING_DIR`.
- **`~/.infer/projects/<project-slug>/tmp/`** - scratch space for tools
  (Write streaming chunks, dynamic skills, clipboard images, screenshots,
  ...). Safe to delete when the CLI is idle.
- **`~/.infer/projects/<project-slug>/history/history`** - chat input
  history, one command per line (per-agent files: `history-<name>`).
  Powers inline auto-completion.
- **`~/.infer/projects/<project-slug>/backups/`** - file backups created by
  the Write/Edit tools before overwriting an existing file.
- **`~/.infer/projects/<project-slug>/artifacts/`** - agent deliverables
  (generated images, downloads, A2A artifacts), grouped per session.
- **`~/.infer/projects/<project-slug>/exports/`** - `chat_export_*.md`
  files written by `infer export` (default when `export.output_dir` is
  unset).
- **`~/.infer/plans/<timestamp>-<slug>.md`** *(userspace)* - plans persisted
  by the `RequestPlanApproval` tool when the agent runs in
  [Plan Mode](plan-mode.md). Both accepted and rejected plans are kept as
  an audit trail.
- **`~/.infer/schedules/<id>.yaml`** *(userspace)* - one YAML per scheduled job.
  Written by the `Schedule` tool, hot-reloaded by the
  daemon. See [Scheduling](scheduling.md).
- **`~/.infer/tmp/tts/`** *(userspace)* - generated speech WAVs, the default of
  `text_to_speech.output_dir`. See [Text to Speech](text-to-speech.md).
- **`~/.infer/tmp/music/`** *(userspace)* - generated music MP3s, the default of
  `text_to_music.output_dir`. See [Text to Music](text-to-music.md).
- **`~/.infer/tmp/sfx/`** *(userspace)* - generated sound-effect WAVs, the default
  of `text_to_sfx.output_dir`. See
  [Text to SFX](tools-reference.md#texttosfx-tool).
- **`~/.infer/tmp/voice/`** *(userspace)* - retained inbound voice/audio
  recordings, the default of `speech_to_text.recordings_dir` when
  `retain_recordings` is greater than 0. See [Speech to
  Text](speech-to-text.md).
- **`~/.infer/tmp/media/`** *(userspace)* - retained inbound Telegram
  photo/video attachments, the default of `channels.telegram.media.dir`.
  See [Channels](channels.md).
- **`~/.infer/telemetry/`** *(userspace)* - usage stats backing
  [`infer stats`](commands-reference.md); wiped by `/reset`. See
  [Telemetry](telemetry.md).
- **`~/.infer/run/`** *(userspace)* - daemon pid/lock files; wiped by
  `/reset`.

The whole `~/.infer/tmp/` tree is listed in `UserspaceRuntimeDirNames`, so the
agent's file tools can read and write it (that is the point of retaining
recordings and media: they are assets for the agent, and generated speech is
deliverable output), while the rest of `~/.infer/` stays protected. `/reset`
empties the tree through the `tmp` parent and does not recreate the
subdirectories - the owning subsystems recreate them on next use.

### Existing Installs

Before this layout change the three media dirs lived directly under
`~/.infer/` (`tts/`, `voice/`, `media/`). They hold only disposable output
(retained recordings and media, generated speech), so nothing migrates
automatically: delete the old directories, or `mv` their contents under
`~/.infer/tmp/` if you want to keep the retained files.

---

## What to Commit, What to Ignore

Any committed configuration lives in the *optional* project `.infer/` - which
only exists if you created overrides there (the CLI never populates it).
Runtime artifacts live under `~/.infer/projects/` and are never written to
the project directory. The general guidance:

**Commit** (project-shareable configuration):

- `.infer/config.yaml`, `prompts.yaml`, `keybindings.yaml`,
  `channels.yaml`, `computer_use.yaml`, `browser_use.yaml`, `agents.yaml`,
  `mcp.yaml`
- `.infer/shortcuts/`

**Don't commit** (machine-local or contains secrets):

- `~/.infer/` - userspace config is per-user, never per-project
- Anything under [Created at Runtime](#created-at-runtime) above
- Any file containing API keys - prefer `%ENV_VAR%`
  [substitution](configuration-reference.md#environment-variable-substitution)
  or `INFER_*` environment variables

---

[← Back to README](../README.md)
