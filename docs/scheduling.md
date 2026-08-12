# Scheduling Guide

[← Back to README](../README.md)

The `Schedule` tool lets the LLM create tasks that run on a cron schedule. Every
fire runs a real agent and records the run to storage; when the job was created
from a messaging channel (e.g. Telegram), the output is additionally delivered
back through that channel. Typical use cases: *"every morning at 8 AM, send me an
inspiring quote"* from a chat with the bot, or record-only background jobs
created from any session whose results are read from storage.

## How it works

```text
┌──────────────────────────────────────────────────────────────┐
│  infer daemon (long-running)                                 │
│                                                              │
│   SchedulerService                                           │
│    ├─ robfig/cron/v3 scheduler                               │
│    ├─ 2s poll + diff against storage backend                 │
│    └─ on fire: persist RunRecord, spawn                      │
│       `infer headless --session-id <uuid>`,                  │
│       emit run events ──► ScheduleNotifier                   │
│                            └─ job has a channel? Send(...)   │
│   ChannelManagerService                                      │
│    └─ inbound msgs → spawn `infer headless`                  │
└──────────────────────────────────────────────────────────────┘
           ▲                                       ▲
           │ writes via storage backend            │ reads via storage backend
┌──────────┴───────────┐                 ┌─────────┴──────────────┐
│ Schedule tool        │ create / update │ Storage backend          │
│ (runs in any agent)  │ ──────────────► │ (sqlite, postgres,       │
│                      │                 │  redis, jsonl, memory)   │
└──────────────────────┘                 └────────────────────────┘
```

Key properties:

- **Backend-agnostic storage.** The `Schedule` tool and the scheduler both use the
  configured storage backend (sqlite, postgres, redis, jsonl, or memory). Jobs are
  persisted through the `ScheduledJobStorage` interface.
- **Hot reload.** The scheduler polls the storage backend every 2 seconds and
  diffs the jobs against its cron entries: created, updated (including hand-edited
  YAML on the jsonl backend), and deleted jobs are picked up within ~2s. The
  scheduler refuses to start on the memory backend (`storage.enabled: false`),
  since a per-process store can never be seen by the daemon.
- **Fresh session per fire.** Each scheduled run gets a brand-new agent session ID.
  Nothing carries between fires; design prompts to be self-contained.
- **Run records.** Every fire persists a `RunRecord` (`session_id`, `job_id`,
  `status`, `error`, timestamps) through the storage backend. The `session_id` is
  the conversation ID of that run, so the full transcript is readable from
  conversation storage - this is how non-channel consumers (e.g. the desktop app)
  pick up job output. The newest 200 records are retained.
- **Optional delivery.** `channel`/`recipient_id` on a job are a delivery target,
  not a requirement. Jobs created from a channel session deliver their output back
  to that channel; jobs created anywhere else are record-only.
- **Daemon-bound execution.** Jobs only fire while `infer daemon` is running.

## Setup

### 1. Enable the tool

Add to `.infer/config.yaml` (or `~/.infer/config.yaml` for user-wide defaults):

```yaml
tools:
  enabled: true
  schedule:
    enabled: true               # off by default
    require_approval: true      # default; require_approval is highly recommended
    max_jobs: 100               # safety cap
```

You can also use environment variables:

```bash
export INFER_TOOLS_SCHEDULE_ENABLED=true
```

### 2. (Optional) Configure a channel

Channel delivery is optional - without one, jobs are record-only. To have job
output delivered to a messaging platform, set up Telegram (or any other supported
channel) following [Channels Guide](channels.md) and create the job from a chat on
that channel. The tool errors when a channel-driven session references a channel
that isn't enabled - a misconfiguration, not a record-only job.

### 3. Run the daemon

```bash
infer daemon
```

You should see a log line like `Scheduler started jobs=0`.

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

The full grammar (including `@every`, `@daily`, `@hourly` descriptors) is documented
at [robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3#hdr-CRON_Expression_Format).

## GitHub backend

Set `scheduler.backend: github` to run schedules on GitHub Actions instead of a
local daemon - cloud scheduling with zero user-owned infrastructure. Each job is
materialized as one scheduled workflow (`.github/workflows/<job-id>.yml`) in a
repository you configure; the workflow runs the job's prompt via
[`inference-gateway/infer-action`](https://github.com/inference-gateway/infer-action)
under your infer GitHub App's bot identity.

```yaml
# .infer/config.yaml
scheduler:
  backend: github
  github:
    repository: "" # "" => <your login>/.routines, created private on first save
    pull_requests: false # true => deploy via PR instead of pushing to main
    artifacts:
      enabled: true
      poll_interval: 10m
      initial_delay: 1m
      max_attempts: 3 # download attempts per artifact, then skipped
      rate_limit_backoff: 1h # pause after a rate-limited GitHub API call
```

How it behaves:

- **Save → deploy.** Creating, updating, or deleting a job clones the repo,
  writes (or removes) that job's workflow file, and pushes the commit to the
  default branch. With `pull_requests: true` the change lands on a branch and a
  PR is opened instead - merging deploys, and the PR is the review step and
  audit trail.
- **Repo auto-creation.** If the configured repository does not exist it is
  created private (default name `.routines` under the authenticated user). All
  GitHub access goes through the `gh` CLI, so `gh auth login` must be done once.
- **Cron is translated.** GitHub Actions cron is UTC-only, 5-field, minimum
  5-minute interval. Descriptors are translated (`@daily` → `0 0 * * *`,
  `@every 10m` → `*/10 * * * *`, ...); expressions that cannot be expressed
  (e.g. `@every 7m`, `* * * * *`) are rejected at save time with a clear error.
- **One-off jobs.** `run_once` jobs render a final step that disables the
  workflow after its first fire (the file stays in the repo, disabled).
- **Conversation pull-back.** The workflow uploads the run's conversation
  `*.jsonl` files as an Actions artifact (`infer-conversations-<run_id>`). While
  `infer daemon` is running, an artifact poller downloads new artifacts into
  local conversation storage on the configured interval (jsonl storage backend
  only). Each artifact gets up to `max_attempts` download attempts; a
  rate-limited API call pauses polling for `rate_limit_backoff`.
- **Local list stays authoritative for the CLI.** Jobs are still recorded
  locally, so `list`/`get`/`update`/`delete` work as usual. A failed GitHub sync
  aborts the save entirely - no phantom jobs.

### Required repository secrets

The CLI never writes secrets. Set these as Actions secrets on the routines
repository (Settings → Secrets and variables → Actions):

- `APP_CLIENT_ID` / `APP_PRIVATE_KEY` - your infer GitHub App's client ID and
  private key; the workflow mints an installation token with
  `actions/create-github-app-token` and runs infer-action as the App bot. For
  `run_once` self-disabling, the App needs the **Actions (read & write)**
  repository permission.
- The API key secret(s) for your provider(s): `ANTHROPIC_API_KEY`,
  `OPENAI_API_KEY`, `GOOGLE_API_KEY`, `DEEPSEEK_API_KEY`, `GROQ_API_KEY`,
  `MISTRAL_API_KEY`, `CLOUDFLARE_API_KEY`, `COHERE_API_KEY`,
  `OLLAMA_CLOUD_API_KEY`, `MOONSHOT_API_KEY`, `MINIMAX_API_KEY`,
  `NVIDIA_API_KEY`, `ZAI_API_KEY`. Only the ones for the models your jobs use
  are needed.

Out of scope for this backend (first cut): channel delivery of job output,
syncing runs into local `RunRecord`s.

## Tool operations

The `Schedule` tool is a single tool with an `operation` parameter. The LLM picks
the operation at call time.

### create - recurring

Required: `cron_expression`, `prompt`. Optional: `run_once`, `name`, `description`,
`model`.

Channel and recipient are **derived automatically** from the current session
(format: `channel-<name>-<sender_id>`). The LLM never passes them. Outside a
channel-driven session (chat, headless, desktop) the job is created without a
delivery target and its output is read from storage via the run records.

```json
{
  "operation": "create",
  "cron_expression": "0 8 * * *",
  "prompt": "Find an inspiring quote for today and respond with the quote and its author. Keep it under 3 sentences.",
  "name": "Daily morning quote",
  "description": "Wake-up quote"
}
```

### create - one-off

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

The tool returns the generated job ID. The job is persisted through the configured
storage backend and the running daemon picks it up within ~2s via its poll loop.

### list

```json
{ "operation": "list" }
```

Returns all jobs sorted by creation time, including their `last_run` and `last_error`
fields when available.

### get

```json
{ "operation": "get", "job_id": "0a1b2c3d-..." }
```

### update

Provide `job_id` and any of: `cron_expression`, `prompt`, `run_once`, `name`,
`description`, `model`. Untouched fields are preserved.

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

## End-to-end Telegram example - recurring

1. **User (Telegram):** *"Can you send me an inspiring quote every day at 8 AM?"*
2. **Bot:** *"Sure - should this run every day from now on, or just once tomorrow?"*
3. **User:** *"Every day."*
4. **Bot calls `Schedule` tool** with:
   - `operation=create`
   - `cron_expression="0 8 * * *"`
   - `prompt="Find one inspiring quote and respond with quote + author, max 3 sentences."`
   - (channel + recipient are derived from the session ID - not passed)
5. **User approves** (because `require_approval: true`).
6. **Bot:** *"Done - job 01HG... scheduled. I'll message you tomorrow at 8 AM UTC."*
7. **At 08:00 UTC the next day**, the daemon fires the job: spawns a fresh
   `infer headless` session with the saved prompt, captures the assistant's response,
   and sends it to the user via Telegram.

## End-to-end Telegram example - one-off

1. **User (Telegram):** *"Remind me at 6pm today to call mum."*
2. **Bot:** *"Got it - should this be a one-off reminder for today, or recurring every day at 6pm?"*
3. **User:** *"Just once, today."*
4. **Bot calls `Schedule` tool** with:
   - `operation=create`
   - `cron_expression="0 18 26 4 *"` (6pm on April 26)
   - `prompt="Remind me to call mum."`
   - `run_once=true`
5. **User approves**.
6. **Bot:** *"Done - I'll ping you at 6pm today."*
7. **At 18:00**, the daemon fires the job, sends the reminder, and deletes the
   job (because `run_once=true`). Next April 26 it will not fire again.

## Troubleshooting

**Jobs aren't firing.**

- Make sure `infer daemon` is running and `Scheduler started` appears
  in the logs.
- Check that the channel referenced in the job is enabled in config.
- Inspect the job's `last_error` field after the expected fire time.

**Jobs fire but no message arrives.**

- The agent may have been silent (no assistant content). Check daemon logs for
  `Failed to send scheduled-job output`.
- Check that the channel is registered (`Registered channel channel=telegram` log
  line on daemon startup).

## Security considerations

- **Approval required by default.** The LLM cannot create/modify/delete jobs
  without explicit user confirmation. Keep `tools.schedule.require_approval: true`
  unless you fully trust the channel.
- **Full agent capabilities at fire time.** Each fire is a real agent session -
  it can read files, call other tools, etc. Do not schedule prompts that would
  do anything sensitive without explicit narrow framing.
- **Per-channel allowlists still apply.** The schedule tool only lets the LLM
  create jobs targeting channels that are enabled in config; per-channel
  `allowed_users` still gates inbound interactions.
