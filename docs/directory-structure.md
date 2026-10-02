# Directory Structure

[← Back to README](../README.md)

This page is a map of the main files and subdirectories the `infer` CLI reads or
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
├── sandbox.yaml          # sandbox policy: filesystem.allowed and filesystem.denied
├── tools.yaml            # tool approval policy and bash allow-list
├── channels.yaml         # remote messaging channels (Telegram, ...)
├── computer_use.yaml     # computer-use / vision settings
├── browser_use.yaml      # browser automation (Playwright) settings
├── agents.yaml           # A2A agent registry
├── agents/               # Markdown subagent definitions (<name>.md), see docs/subagents.md
├── mcp.yaml              # MCP server registry
├── memory.yaml           # persistent memory settings, see docs/memory.md
├── heartbeat.yaml        # heartbeat prompt and interval, see docs/heartbeat.md
├── judge.yaml            # LLM judge settings, see docs/judge-mode.md
├── hooks.yaml            # agent-loop command hooks (disabled by default)
├── reminders.yaml        # system reminders (enabled by default)
├── memory/               # memory fact-files and the MEMORY.md index
├── shortcuts/            # /-prefixed chat shortcuts (built-in + custom)
│   ├── git.yaml
│   ├── scm.yaml
│   ├── mcp.yaml
│   ├── shells.yaml
│   ├── export.yaml
│   ├── env.yaml
│   ├── skills.yaml
│   ├── reset.yaml
│   └── insights.yaml
├── skills/               # Agent Skills - SKILL.md folders, see docs/skills.md
├── tools/                # custom tool manifests (<Name>.yaml), see docs/custom-tools.md
├── avatars/              # TextToVideo avatar library: <name>/ folders of portrait images; survives /reset
├── schedules/            # cron-driven scheduled jobs (one YAML per job)
├── plans/                # plan-mode plans saved by RequestPlanApproval (one .md per plan)
├── insights/             # /insights session reports (one .md per run); survives /reset
├── logs/                 # CLI + gateway logs (app/daemon/gateway <date>.log)
├── telemetry/            # usage stats backing `infer stats` (see docs/telemetry.md)
├── run/                  # daemon pid/lock files
├── tmp/                  # userspace scratch: agent-readable/writable, wiped by /reset
│   └── media/            # media root when no project is open (see Media Directories)
├── bin/                  # downloaded gateway binary, one shared copy per machine
├── conversations.db      # shared SQLite conversation store (type: sqlite)
├── artifacts/            # GitHub artifact poller downloads (see infer daemon)
├── models/               # whisper/ and tts/ speech models
└── projects/             # per-project runtime state, grouped by project
    └── <project-slug>/
        ├── conversations/  # JSONL conversation stores (type: jsonl)
        ├── history/        # chat input history (one entry per line)
        ├── backups/        # file-write tool backups
        ├── tmp/            # scratch space (streamed writes, dynamic skills, ...)
        │   └── media/      # media root while this project is open (see Media Directories)
        ├── artifacts/      # agent deliverables (images, downloads, ...)
        └── exports/        # `infer export` chat markdown exports

.infer/                   # OPTIONAL project layer - config overrides only,
│                         # created by you / `infer config set --project`
│                         # (the CLI never writes here on its own)
├── config.yaml           # sparse override of ~/.infer/config.yaml
├── mcp.yaml              # project MCP servers (project-then-home lookup)
├── keybindings.yaml      # project keybindings (project-then-home lookup)
├── shortcuts/            # project shortcuts, overlaid by name onto ~/.infer/shortcuts/
├── skills/               # project skills, still discovered when present
├── tools/                # project custom tools (<Name>.yaml), always need approval, see docs/custom-tools.md
└── agents/               # project Markdown subagents (override ~/.infer/agents/ by name)

.agents/                  # open-standard project layer (cross-tool skills and tools)
├── skills/               # Agent Skills - SKILL.md folders (read-only discovery)
│   └── <name>/SKILL.md   # e.g. .agents/skills/pdf/SKILL.md
└── tools/                # project custom tools, like .infer/tools/ (which wins on a name clash)
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
  conversation, init, vision).
- **`keybindings.yaml`** - keyboard shortcuts for the chat TUI. Edit via
  `infer keybindings set/disable/reset` or by hand.
- **`sandbox.yaml`** - the sandbox policy, one section per resource. Under
  `filesystem`, `allowed` paths the file tools may use (optionally `access: read`)
  and `denied` paths they never may (optionally `on_violation: approval`).
  Userspace only, a project copy is ignored, and the agent's file tools cannot
  write this file, whatever it says.
- **`tools.yaml`** - the tool approval policy: per-tool `enabled` flags,
  `require_approval` overrides, `safety` and the per-mode bash allow-list.
  Userspace only, a project copy is ignored, and the agent's file tools cannot
  write this file. A `tools:` block in `config.yaml` has no effect.
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
- **`hooks.yaml`** - command hooks the agent runs at hook points, disabled by
  default. See [Reminders & Command Hooks](hooks.md).
- **`reminders.yaml`** - system reminders injected at hook points, enabled by
  default. See [Reminders & Command Hooks](hooks.md).
- **`heartbeat.yaml`** - the heartbeat prompt and interval. See [Heartbeat](heartbeat.md).
- **`judge.yaml`** - the LLM judge model, prompt and on-error policy. See
  [Judge Mode](judge-mode.md).
- **`memory.yaml`** - persistent memory settings (directory, index cap, backend).
  See [Persistent Memory](memory.md).
- **`shortcuts/*.yaml`** - `/git`, `/scm`, `/mcp`, `/shells`, `/export`, `/env`,
  `/skills`, `/reset`, `/insights` shortcuts plus any you add. Drop new YAML files into
  `shortcuts/`. A project `./.infer/shortcuts/` is overlaid on top by shortcut
  name, so it adds to (or replaces individual entries of) the userspace set
  rather than hiding it. See [Shortcuts Guide](shortcuts-guide.md).
- **`skills/`** - Agent Skills directory. Drop a `SKILL.md` folder here (or
  into the cross-tool `.agents/skills/` open standard) to extend the agent.
  See [Skills](skills.md).
- **`agents/`** - Markdown subagent definitions you create yourself, one
  `<name>.md` per agent (frontmatter plus a system-prompt body, Claude Code /
  Gemini CLI compatible); not seeded by init. A project `.infer/agents/`
  overrides a same-named file here. See [Markdown Subagents](subagents.md).

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
  (Write streaming chunks, dynamic skills, clipboard images, the project's
  media root, ...). Safe to delete when the CLI is idle.
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
- **`<media root>/`** - generated and retained media, one subdirectory per
  kind. See [Media Directories](#media-directories).
- **`~/.infer/avatars/`** *(userspace)* - the avatar library: one folder per avatar
  holding one or more portrait images, managed with `infer avatars create|list|delete` and
  kept by `/reset`. See [Text to Video](text-to-video.md#avatar-library).
- **`~/.infer/artifacts/`** *(userspace)* - GitHub artifact downloads fetched by
  `infer daemon`'s artifact poller.
- **`~/.infer/models/`** *(userspace)* - speech models downloaded on first use:
  `whisper/` for speech-to-text and `tts/` for text-to-speech. See
  [Speech to Text](speech-to-text.md) and [Text to Speech](text-to-speech.md).
- **`~/.infer/telemetry/`** *(userspace)* - usage stats backing
  [`infer stats`](commands-reference.md); wiped by `/reset`. See
  [Telemetry](telemetry.md).
- **`~/.infer/run/`** *(userspace)* - daemon pid/lock files; wiped by
  `/reset`.

### Media Directories

Generated and retained media share one media root with a subdirectory per
kind. The root follows the open project: it is
`~/.infer/projects/<project-slug>/tmp/media/` while the CLI runs in a project
(the desktop app runs a selected project's sessions there). It falls back to
`~/.infer/tmp/media/` when no project is open, that is when the working
directory is `$HOME` or the desktop's `~/.infer/workspace`.

| Subdirectory | Holds | Config override |
| --- | --- | --- |
| `tts/` | generated speech WAVs ([Text to Speech](text-to-speech.md)) | `text_to_speech.output_dir` |
| `music/` | generated music MP3s ([Text to Music](text-to-music.md)) | `text_to_music.output_dir` |
| `sfx/` | generated sound effects ([TextToSFX](tools-reference.md#texttosfx-tool)) | `text_to_sfx.output_dir` |
| `video/` | generated video MP4s ([Text to Video](text-to-video.md)) | `text_to_video.output_dir` |
| `recordings/` | `RecordStart` screen recordings ([RecordStart and RecordStop](tools-reference.md#recordstart-and-recordstop-tools)) | `computer_use.recording.output_dir` |
| `screenshots/` | browser and computer-use screenshots | `computer_use.screenshot.temp_dir` |
| `voice/` | retained inbound voice recordings ([Speech to Text](speech-to-text.md)) | `speech_to_text.recordings_dir` |
| `attachments/` | retained inbound Telegram photos and videos ([Channels](channels.md)) | `channels.telegram.media.dir` |

Both tmp trees are listed in `RuntimeArtifactDirNames` and
`UserspaceRuntimeDirNames`, so the agent's file tools can read and write the
media root (that is the point of retaining recordings and media: they are
assets for the agent, and generated speech is deliverable output), while the
rest of `~/.infer/` stays protected. `/reset` empties both trees through their
`tmp` parents and does not recreate the subdirectories - the owning subsystems
recreate them on next use.

### Existing Installs

Older releases placed the media dirs directly under `~/.infer/` or
`~/.infer/tmp/` (`tts/`, `voice/`, `media/`, ...). They hold only disposable
output, so nothing migrates automatically: delete the old directories, or `mv`
their contents into the media root if you want to keep the retained files.

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
- Any file containing API keys - keep secrets out of `config.yaml` (its values
  are not expanded) and supply them via `INFER_*` environment variables or
  `~/.infer/auth.yaml`

---

[← Back to README](../README.md)
