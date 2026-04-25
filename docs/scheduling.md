# Scheduling Guide

[← Back to README](../README.md)

The `Schedule` tool lets the LLM create recurring tasks that run on a cron schedule
and deliver their output back to the user through a configured messaging channel
(e.g. Telegram). It is designed for use cases like *"every morning at 8 AM, send me
an inspiring quote"* — initiated from a chat with the bot rather than from the CLI.

## How it works

```text
┌─────────────────────────────────────────────────────────────┐
│  infer channels-manager (long-running daemon)               │
│                                                              │
│   ChannelManagerService                                      │
│    ├─ inbound msgs   → spawn `infer agent`                   │
│    └─ SchedulerService                                       │
│         ├─ robfig/cron/v3 scheduler                          │
│         ├─ fsnotify watcher on ~/.infer/schedules/           │
│         └─ on fire: spawn `infer agent --session-id <uuid>`  │
│                     capture stdout → channel.Send(...)       │
└─────────────────────────────────────────────────────────────┘
           ▲                                       ▲
           │ writes YAML                           │ reads YAML on startup
┌──────────┴───────────┐                 ┌─────────┴──────────────┐
│ Schedule tool        │ create / update │ ~/.infer/schedules/    │
│ (runs in any agent)  │ ──────────────► │   <job-id>.yaml        │
└──────────────────────┘                 └────────────────────────┘
```

Key properties:

- **Tool-only file I/O.** The `Schedule` tool only reads/writes YAML files — it never directly talks to the daemon.
- **Hot reload.** The daemon's `fsnotify` watcher picks up new/changed/deleted job files within ~150ms and registers them with the cron scheduler.
- **Fresh session per fire.** Each scheduled run gets a brand-new agent session ID. Nothing carries between fires; design prompts to be self-contained.
- **Daemon-bound execution.** Jobs only fire while `infer channels-manager` is running.

## Setup

### 1. Enable the tool

Add to `.infer/config.yaml` (or `~/.infer/config.yaml` for user-wide defaults):

```yaml
tools:
  enabled: true
  schedule:
    enabled: true               # off by default
    require_approval: true      # default; require_approval is highly recommended
    storage_dir: ""             # default: ~/.infer/schedules
    max_jobs: 100               # safety cap
```

You can also use environment variables:

```bash
export INFER_TOOLS_SCHEDULE_ENABLED=true
```

### 2. Configure at least one channel

The Schedule tool refuses to create a job for a channel that isn't enabled. Set up Telegram (or any other supported channel) following [Channels Guide](channels.md).

### 3. Run the daemon

```bash
infer channels-manager
```

You should see a log line like `Scheduler started storage_dir=/home/you/.infer/schedules`.

## Cron syntax

Standard 5-field crontab format: `minute hour day-of-month month day-of-week`.

| Expression     | Meaning                          |
| -------------- | -------------------------------- |
| `0 8 * * *`    | Every day at 08:00               |
| `*/15 * * * *` | Every 15 minutes                 |
| `0 9 * * 1-5`  | Weekdays at 09:00                |
| `0 0 1 * *`    | First of every month at midnight |
| `@every 1h`    | Every hour                       |
| `@every 30m`   | Every 30 minutes                 |
| `@daily`       | Equivalent to `0 0 * * *`        |

The full grammar (including `@every`, `@daily`, `@hourly` descriptors) is documented at [robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format).

## Tool operations

The `Schedule` tool is a single tool with an `operation` parameter. The LLM picks the operation at call time.

### create — recurring

Required: `cron_expression`, `prompt`. Optional: `run_once`, `name`, `description`, `model`.

Channel and recipient are **derived automatically** from the current session
(format: `channel-<name>-<sender_id>`). The LLM never passes them. The tool
errors out when invoked outside a channel-driven session.

```json
{
  "operation": "create",
  "cron_expression": "0 8 * * *",
  "prompt": "Find an inspiring quote for today and respond with the quote and its author. Keep it under 3 sentences.",
  "name": "Daily morning quote",
  "description": "Wake-up quote"
}
```

### create — one-off

Set `run_once: true` to make the scheduler delete the job after its first
fire. The LLM is instructed to **always confirm with the user whether they
want a one-off or recurring job** before creating one.

```json
{
  "operation": "create",
  "cron_expression": "0 18 26 4 *",
  "prompt": "Remind me to call mum.",
  "run_once": true,
  "name": "Call mum reminder"
}
```

The tool returns the generated job ID. The job is written to `~/.infer/schedules/<id>.yaml` and the running daemon picks it up via fsnotify within ~150ms.

### list

```json
{ "operation": "list" }
```

Returns all jobs sorted by creation time, including their `last_run` and `last_error` fields when available.

### get

```json
{ "operation": "get", "job_id": "0a1b2c3d-..." }
```

### update

Provide `job_id` and any of: `cron_expression`, `prompt`, `run_once`, `name`, `description`, `model`. Untouched fields are preserved.

```json
{
  "operation": "update",
  "job_id": "0a1b2c3d-...",
  "cron_expression": "0 9 * * *"
}
```

### delete

```json
{ "operation": "delete", "job_id": "0a1b2c3d-..." }
```

## YAML job file format

```yaml
id: 01HG7K2N3M4P5Q6R7S8T9V0W1X
name: Daily morning quote
description: Wake-up quote for user 12345
cron_expression: "0 8 * * *"
prompt: |
  Find an inspiring quote for today and respond with the quote and its author.
  Keep it under 3 sentences.
channel: telegram
recipient_id: "12345"
model: ""                   # empty = use agent.model from config
run_once: false             # true → deleted after first fire
created_at: 2026-04-25T10:30:00Z
updated_at: 2026-04-25T10:30:00Z
last_run: 2026-04-26T08:00:01Z
last_error: ""
```

The daemon updates `last_run` and `last_error` after each fire (recurring jobs only — one-off jobs are deleted instead).

## End-to-end Telegram example — recurring

1. **User (Telegram):** *"Can you send me an inspiring quote every day at 8 AM?"*
2. **Bot:** *"Sure — should this run every day from now on, or just once tomorrow?"*
3. **User:** *"Every day."*
4. **Bot calls `Schedule` tool** with:
   - `operation=create`
   - `cron_expression="0 8 * * *"`
   - `prompt="Find one inspiring quote and respond with quote + author, max 3 sentences."`
   - (channel + recipient are derived from the session ID — not passed)
5. **User approves** (because `require_approval: true`).
6. **Bot:** *"Done — job 01HG... scheduled. I'll message you tomorrow at 8 AM UTC."*
7. **At 08:00 UTC the next day**, the daemon fires the job: spawns a fresh
   `infer agent` session with the saved prompt, captures the assistant's response,
   and sends it to the user via Telegram.

## End-to-end Telegram example — one-off

1. **User (Telegram):** *"Remind me at 6pm today to call mum."*
2. **Bot:** *"Got it — should this be a one-off reminder for today, or recurring every day at 6pm?"*
3. **User:** *"Just once, today."*
4. **Bot calls `Schedule` tool** with:
   - `operation=create`
   - `cron_expression="0 18 26 4 *"` (6pm on April 26)
   - `prompt="Remind me to call mum."`
   - `run_once=true`
5. **User approves**.
6. **Bot:** *"Done — I'll ping you at 6pm today."*
7. **At 18:00**, the daemon fires the job, sends the reminder, and deletes the
   YAML file (because `run_once=true`). Next April 26 it will not fire again.

## Troubleshooting

**Jobs aren't firing.**

- Make sure `infer channels-manager` is running and `Scheduler started` appears in the logs.
- Check that the channel referenced in the job is enabled in config.
- Inspect the YAML file's `last_error` field after the expected fire time.

**Jobs fire but no message arrives.**

- The agent may have been silent (no assistant content). Check daemon logs for `Failed to send scheduled-job output`.
- Check that the channel is registered (`Registered channel channel=telegram` log line on daemon startup).

**Editing the YAML by hand.**

- Saving a `<id>.yaml` file (write + rename, as most editors do) triggers fsnotify and the daemon re-registers the job. No restart needed.
- Deleting a `<id>.yaml` file also triggers fsnotify and unregisters the job.

## Security considerations

- **Approval required by default.** The LLM cannot create/modify/delete jobs
  without explicit user confirmation. Keep `tools.schedule.require_approval: true`
  unless you fully trust the channel.
- **Full agent capabilities at fire time.** Each fire is a real agent session —
  it can read files, call other tools, etc. Do not schedule prompts that would
  do anything sensitive without explicit narrow framing.
- **Per-channel allowlists still apply.** The schedule tool only lets the LLM
  create jobs targeting channels that are enabled in config; per-channel
  `allowed_users` still gates inbound interactions.
