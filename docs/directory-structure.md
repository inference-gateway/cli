# Directory Structure

[← Back to README](../README.md)

This page is a map of every file and subdirectory the `infer` CLI reads or
writes. It complements [Configuration Reference](configuration-reference.md),
which documents what each *option* does - this page documents where each
*file* lives and why it exists.

## Table of Contents

- [The Two Layers](#the-two-layers)
- [At a Glance](#at-a-glance)
- [Files Seeded by `infer init`](#files-seeded-by-infer-init)
- [Created at Runtime](#created-at-runtime)
- [What to Commit, What to Ignore](#what-to-commit-what-to-ignore)

---

## The Two Layers

The CLI keeps state in two locations:

- **Project layer** - `.infer/`, sitting next to your code. Scoped to the
  current project.
- **Userspace layer** - `~/.infer/`, in your home directory. A global
  fallback shared across all projects.

Both layers contain the same *set* of files. Project values override
userspace values. Run `infer init` for the project layer or
`infer init --userspace` for the userspace layer - both create an
identical seed set. See
[Configuration Layers](configuration-reference.md#configuration-layers)
for the full precedence rules.

---

## At a Glance

```text
.infer/                   # project layer (also mirrored at ~/.infer/)
├── config.yaml           # main configuration
├── prompts.yaml          # LLM system prompts (agent, git, conversation, tools, ...)
├── keybindings.yaml      # chat UI keyboard shortcuts
├── channels.yaml         # remote messaging channels (Telegram, ...)
├── computer_use.yaml     # computer-use / vision settings
├── agents.yaml           # A2A agent registry
├── mcp.yaml              # MCP server registry
├── shortcuts/            # /-prefixed chat shortcuts (built-in + custom)
│   ├── git.yaml
│   ├── scm.yaml
│   ├── mcp.yaml
│   ├── shells.yaml
│   ├── export.yaml
│   └── a2a.yaml
├── .gitignore            # ignores the runtime-generated files below
│
│   # --- created at runtime, not by `infer init` ---
├── conversations.db      # SQLite conversation store (when storage.type=sqlite)
├── conversations/        # JSONL conversation store (when storage.type=jsonl)
├── logs/                 # debug / error logs
├── tmp/                  # scratch space for tools (exports, streamed writes, ...)
├── bin/                  # downloaded gateway binary (binary mode)
├── plans/                # plan-mode plans saved by RequestPlanApproval (one .md per plan)
└── history               # chat input history (one entry per line)

~/.infer/                 # userspace layer - same set of config files,
                          # plus one extra:
└── schedules/            # cron-driven scheduled jobs (one YAML per job)
```

---

## Files Seeded by `infer init`

These are the files `infer init` writes once and then leaves to you. All of
them exist in both layers (project and userspace).

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
- **`agents.yaml`** - A2A agent registry (URLs, models, env vars). Manage
  via `infer agents add/remove/list` (or `--userspace`). See
  [A2A Agents](agents-configuration.md).
- **`mcp.yaml`** - MCP server registry and liveness probe settings. Manage
  via `infer mcp ...` or by hand. See [MCP Integration](mcp-integration.md).
- **`shortcuts/*.yaml`** - `/git`, `/scm`, `/mcp`, `/shells`, `/export`,
  `/agents` shortcuts plus any you add. Drop new YAML files into
  `shortcuts/`. See [Shortcuts Guide](shortcuts-guide.md).
- **`.gitignore`** - pre-populated to exclude the runtime-generated files
  below.

The split into separate YAML files (rather than one giant `config.yaml`) is
deliberate: each concern has its own file so changes stay focused and
reviews stay readable.

---

## Created at Runtime

These are written by the CLI as you use it - `infer init` does **not**
create them, and the seeded `.gitignore` already excludes them.

- **`conversations.db`** *(project)* - SQLite conversation store, active
  when `storage.type: sqlite`. See
  [Conversation Storage](conversation-storage.md).
- **`conversations/*.jsonl`** *(project)* - JSONL conversation store,
  active when `storage.type: jsonl`. One file per conversation.
- **`logs/`** *(project)* - debug and error logs. Path configurable via
  `logging.dir` / `INFER_LOGGING_DIR`.
- **`tmp/`** *(project)* - scratch space for tools (Write streaming chunks,
  exports, ...). Safe to delete when the CLI is idle.
- **`bin/`** *(project)* - downloaded gateway binary, used when running in
  binary mode (`gateway.docker: false`).
- **`history`** *(project)* - chat input history, one command per line.
  Powers inline auto-completion.
- **`plans/<timestamp>-<slug>.md`** *(project)* - plans persisted by the
  `RequestPlanApproval` tool when the agent runs in [Plan Mode](plan-mode.md).
  Both accepted and rejected plans are kept as an audit trail.
- **`schedules/<id>.yaml`** *(userspace)* - one YAML per scheduled job.
  Written by the `Schedule` tool, hot-reloaded by the channels-manager
  daemon. See [Scheduling](scheduling.md).

---

## What to Commit, What to Ignore

The seeded `.gitignore` (inside `.infer/`) already excludes the runtime
files. The general guidance:

**Commit** (project-shareable configuration):

- `.infer/config.yaml`, `prompts.yaml`, `keybindings.yaml`,
  `channels.yaml`, `computer_use.yaml`, `agents.yaml`, `mcp.yaml`
- `.infer/shortcuts/`
- `.infer/.gitignore`

**Don't commit** (machine-local or contains secrets):

- `~/.infer/` - userspace config is per-user, never per-project
- Anything under [Created at Runtime](#created-at-runtime) above
- Any file containing API keys - prefer `%ENV_VAR%`
  [substitution](configuration-reference.md#environment-variable-substitution)
  or `INFER_*` environment variables

---

[← Back to README](../README.md)
